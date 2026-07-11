package demo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Store logs demo scan activity to Postgres (Neon in production) — purely for
// operator visibility into "what's being scanned," not a user-facing feature.
// No accounts, no per-user history: that's the real Phase 1 product, not this.
type Store struct {
	db *sql.DB
}

// OpenStore connects and ensures the schema exists.
func OpenStore(ctx context.Context, connStr string) (*Store, error) {
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("demo: open db: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("demo: ping db: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS scan_log (
	id             BIGSERIAL PRIMARY KEY,
	requested_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
	hostname       TEXT NOT NULL,
	error_count    INT,
	warn_count     INT,
	pass_count     INT,
	skip_count     INT,
	info_count     INT,
	remote_ip_hash TEXT,
	error          TEXT
)`)
	if err != nil {
		return fmt.Errorf("demo: migrate: %w", err)
	}
	// CountRecent runs on every request now (it's the rate-limit check), so
	// this index is load-bearing for latency, not just a nice-to-have.
	_, err = s.db.ExecContext(ctx, `
CREATE INDEX IF NOT EXISTS scan_log_ip_time_idx ON scan_log (remote_ip_hash, requested_at)`)
	if err != nil {
		return fmt.Errorf("demo: migrate (index): %w", err)
	}
	return nil
}

// ScanLogEntry is one row: either a successful scan (counts populated, Err
// empty) or a failed/rejected attempt (Err populated) — both are useful
// signal for "what's being scanned," including abuse attempts. ErrorCount and
// WarnCount are the two severities within status:fail, computed by the
// caller from the findings themselves — model.Summary only has one coarse
// "Fail" bucket, which would misrepresent "9 warnings, 1 real error" as
// "10 failed" in the log the same way it briefly did in the dashboard.
type ScanLogEntry struct {
	Hostname                                               string
	RemoteIP                                               string
	ErrorCount, WarnCount, PassCount, SkipCount, InfoCount int
	Err                                                    string
}

// LogScan records one entry. Errors are the caller's to decide whether to
// surface — logging failure should never block the actual scan response.
func (s *Store) LogScan(ctx context.Context, e ScanLogEntry) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO scan_log (hostname, error_count, warn_count, pass_count, skip_count, info_count, remote_ip_hash, error)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.Hostname, e.ErrorCount, e.WarnCount, e.PassCount, e.SkipCount, e.InfoCount, hashIP(e.RemoteIP), nullIfEmpty(e.Err))
	if err != nil {
		return fmt.Errorf("demo: log scan: %w", err)
	}
	return nil
}

// CountRecent returns how many scan attempts (successful or not, but never
// requests that were themselves rejected purely for being rate-limited — see
// the caller) this IP has made within window, ending now. This is the actual
// rate-limit mechanism: reading the same table LogScan writes to, rather than
// an in-process map, is what makes rate limiting correct regardless of
// whether the deployment target guarantees a given visitor's requests land on
// the same warm process twice — an in-memory counter would silently stop
// working the moment that assumption breaks (serverless scale-out, multiple
// Fly instances, anything elastic).
func (s *Store) CountRecent(ctx context.Context, remoteIP string, window time.Duration) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT count(*) FROM scan_log WHERE remote_ip_hash = $1 AND requested_at > $2`,
		hashIP(remoteIP), time.Now().Add(-window)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("demo: count recent: %w", err)
	}
	return n, nil
}

// hashIP truncates a SHA-256 of the client IP rather than storing it raw —
// enough to notice "this is the same repeat visitor" without keeping a
// plainly-identifying address around indefinitely in a log table.
func hashIP(ip string) string {
	if ip == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(ip))
	return hex.EncodeToString(sum[:])[:16]
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
