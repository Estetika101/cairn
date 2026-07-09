# Cairn

**An open-source, self-hostable web QA auditor.** Cairn checks any HTTP-reachable
site for SEO, GEO (generative-engine optimization), accessibility, performance,
security headers, broken links, and structured data — with no mandatory API
keys, no cloud dependency, and no mandatory headless browser. It ships as a
single static Go binary, so it runs anywhere from a Raspberry Pi to a CI runner.

> **Status: walking skeleton complete; runnable.** Cairn is being built from a
> detailed spec (`webqa-SPEC-v4.md`, canonical). The first vertical slice — which
> proves every load-bearing architectural seam at once — is done: the polite
> fetch engine (per-host politeness, robots, per-run cache, fetch budget), the
> `security-headers` (page) and `broken-links` (site) checks, content-hash
> finding IDs, the fail/pass/skipped/info model, console/JSON/Markdown/tasks
> output with a CI exit gate, and a **sandboxed WebAssembly plugin runtime** with
> a byte-exact golden test and a 14-case acceptance suite (green under `-race`).
> So `cairn --config cairn.yaml` produces a real audit of a live site today.
>
> Still to come (fill-in against the proven skeleton): the SEO / GEO /
> accessibility / structured-data modules, Tier 2 (Chromium — Core Web Vitals +
> rendered a11y), `serve`/`watch`/`init`, local run-over-run diff, and multi-site
> concurrency.

## Quick start

```sh
go build -o cairn ./cmd/cairn
./cairn --config slice.yaml     # audits the configured site(s)
```

Exit code is non-zero when a finding at or above `failOn` is present, so it
drops straight into CI.

## What makes it different

- **Honest reporting.** Every result is `pass` / `fail` / `skipped` / `info`,
  and a skipped check is a visible line, never silent — a clean report is not a
  false green. WCAG findings map to specific success criteria and separate what
  a robot can verify from what needs a human.
- **Agent-facing output.** Alongside console/Markdown/JSON, Cairn emits a
  `cairn-tasks.md` checklist purpose-built to hand to a coding assistant, derived
  from the same findings so it never drifts.
- **Well-behaved crawler.** Respects `robots.txt` by default, rate-limits per
  host, and reports a WAF challenge as `blocked` rather than mislabeling it
  "site down."
- **Sandboxed plugins.** Third-party checks are WebAssembly modules with no
  ambient authority — a plugin reaches the network only through the engine's own
  polite, budgeted fetch path. Point `plugins:` at a `.wasm` file in config; it
  runs alongside the built-ins through the identical check interface. A runaway
  plugin is interrupted and recorded as skipped, never allowed to hang the run.
  See `plugins/example-x-powered-by/` for a TinyGo example.

## Two-tier design

- **Tier 1 (core):** HTTP + HTML parsing only. Runs anywhere the binary runs.
- **Tier 2 (optional):** activates only if Chromium is present, adding Core Web
  Vitals and rendered-DOM accessibility checks. Absent → those checks report
  `skipped`, never a failure.

## Build

Requires Go 1.25+.

```sh
go build ./cmd/cairn
go test ./...
```

## License

[MIT](LICENSE) © Peter van Aller. Cairn's auditor and every check module are
free forever; any future hosted/paid product is a separate platform *around* the
auditor, never a paywall over an existing check.
