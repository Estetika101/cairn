package demo

import (
	"context"
	"os"
	"testing"
	"time"
)

// testStore opens a Store against a real local Postgres instance for
// integration testing. Skips (not fails) if VERDICT_TEST_DATABASE_URL isn't
// set, so `go test ./...` stays hermetic for anyone without Postgres
// available — CountRecent's actual SQL correctness matters enough to verify
// against a real database at least once, though, not just assume the query
// is right.
func testStore(t *testing.T) *Store {
	t.Helper()
	connStr := os.Getenv("VERDICT_TEST_DATABASE_URL")
	if connStr == "" {
		t.Skip("VERDICT_TEST_DATABASE_URL not set — skipping Postgres integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := OpenStore(ctx, connStr)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	// Each test gets a clean table — this package's whole log is disposable
	// test data, never anything worth preserving between runs.
	if _, err := s.db.ExecContext(ctx, "TRUNCATE scan_log"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore_LogAndCount(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := s.LogScan(ctx, ScanLogEntry{Hostname: "example.com", RemoteIP: "203.0.113.5", PassCount: 5}); err != nil {
			t.Fatalf("LogScan: %v", err)
		}
	}
	// A different IP must not count toward the first IP's total.
	if err := s.LogScan(ctx, ScanLogEntry{Hostname: "example.com", RemoteIP: "203.0.113.9", PassCount: 5}); err != nil {
		t.Fatalf("LogScan: %v", err)
	}

	n, err := s.CountRecent(ctx, "203.0.113.5", time.Hour)
	if err != nil {
		t.Fatalf("CountRecent: %v", err)
	}
	if n != 3 {
		t.Errorf("CountRecent = %d, want 3 (the other IP's entry must not count)", n)
	}
}

func TestStore_CountRecent_WindowExpiry(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.LogScan(ctx, ScanLogEntry{Hostname: "example.com", RemoteIP: "203.0.113.7", PassCount: 1}); err != nil {
		t.Fatalf("LogScan: %v", err)
	}

	// Within a generous window, the entry counts.
	n, err := s.CountRecent(ctx, "203.0.113.7", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("CountRecent(1h) = %d, want 1", n)
	}

	// Backdate the row to simulate it having aged out of a short window,
	// rather than sleeping in the test.
	if _, err := s.db.ExecContext(ctx, `UPDATE scan_log SET requested_at = now() - interval '2 hours' WHERE remote_ip_hash IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	n, err = s.CountRecent(ctx, "203.0.113.7", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("CountRecent(1h) after backdating = %d, want 0 (entry should have aged out)", n)
	}
}

func TestStore_LogScan_FailedAttemptRecorded(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.LogScan(ctx, ScanLogEntry{Hostname: "evil.example", RemoteIP: "203.0.113.11", Err: "private IP rejected"}); err != nil {
		t.Fatalf("LogScan: %v", err)
	}
	var errText string
	err := s.db.QueryRowContext(ctx, "SELECT error FROM scan_log WHERE remote_ip_hash IS NOT NULL LIMIT 1").Scan(&errText)
	if err != nil {
		t.Fatal(err)
	}
	if errText != "private IP rejected" {
		t.Errorf("error column = %q, want the logged failure reason", errText)
	}
}

func TestStore_MigrateIsIdempotent(t *testing.T) {
	connStr := os.Getenv("VERDICT_TEST_DATABASE_URL")
	if connStr == "" {
		t.Skip("VERDICT_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	// OpenStore runs migrate() internally; opening twice must not error even
	// though the table/index already exist (CREATE ... IF NOT EXISTS).
	s1, err := OpenStore(ctx, connStr)
	if err != nil {
		t.Fatal(err)
	}
	defer s1.Close()
	s2, err := OpenStore(ctx, connStr)
	if err != nil {
		t.Fatalf("second OpenStore (re-migrate) failed: %v", err)
	}
	s2.Close()
}
