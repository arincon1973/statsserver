# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository layout

This repo currently contains a single Go module, `fleet-server/`, at `fleet-server/go.mod` (module `github.com/safelyyou/fleet-server`, Go 1.22). Run all commands from that directory.

## Commands

```bash
cd fleet-server

go run .              # start the server on 127.0.0.1:6733
go build ./...        # compile everything
go vet ./...          # static checks
go test ./...         # run tests (no test files exist yet)
```

There is no test suite yet — see "Possible Future Improvements" in `fleet-server/README.md` for planned test coverage (store aggregate calculations, full-stack `httptest.Server` integration tests).

## Architecture

Fleet Management Metrics Server: a dependency-free Go HTTP server (standard library only) that ingests device heartbeats and video-upload timing samples, and serves aggregated per-device metrics. All state is in-memory and does not survive a restart. Full API/response details and a request-flow diagram live in `fleet-server/README.md` — read it for endpoint contracts; the notes below cover what isn't obvious from a single file.

Request flow: `main.go` wires `net/http`'s method-prefixed mux (`"POST /api/v1/devices/{device_id}/heartbeat"` style patterns, using `r.PathValue()` — no third-party router) through `middleware.RequestLogger` into `handlers.Handler`, which is backed by a single `store.Store`.

- **`main.go`** — builds the `slog` logger (writes to stdout + a timestamped file under `logs/`), seeds the known-device allowlist (`seededDevices`), constructs the store and handlers, and registers routes. Any device ID not in `seededDevices` gets a 404 from every endpoint. To register a new device, add it to `seededDevices` and restart — there's no runtime registration endpoint.
- **`middleware/logging.go`** — wraps every request/response to log one structured line per request (method, path, device_id, request/response bodies, status, duration). It buffers the request body (so handlers can still read it) and captures the response body/status via a `responseRecorder` wrapping `http.ResponseWriter`.
- **`handlers/handlers.go`** — decodes JSON, translates `store.ErrNotFound` into HTTP 404, and returns 500 on other errors. Contains no business logic itself; delegates aggregation to the store.
- **`store/store.go`** — thread-safe in-memory store (`sync.RWMutex`, write lock for heartbeat/stat recording, read lock for queries). Keeps only *running aggregates* per device — never grows a slice of historical samples — so reads are O(1):
  - heartbeats: `firstAt`, `lastAt`, running `total` count
  - uploads: running `totalTime` and `count` (average is computed on read)
  - Uptime formula: `(total heartbeats / minutes between first and last heartbeat) × 100`, capped at 100; a single heartbeat (zero-width window) is treated as 100%.

When changing aggregation logic, update both `store/store.go` and the formula/description in `fleet-server/README.md` so they stay in sync.

## Logs

`fleet-server/logs/` accumulates a new timestamped log file on every server run and is untracked in git (present locally but not committed) — don't assume its contents reflect the latest code, and don't add old runs to commits.
