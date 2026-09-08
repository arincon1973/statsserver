# Fleet Management Metrics Server

A lightweight HTTP server written in Go that collects heartbeat signals and video-upload statistics from a fleet of field devices, and exposes per-device metrics on demand.

---

## Purpose

Field devices periodically send two kinds of telemetry to this server:

- **Heartbeats** — a lightweight ping that lets the server know the device is alive.
- **Upload stats** — the time (in nanoseconds) it took a device to upload a video clip.

The server aggregates this data in memory and answers queries about each device's health:

- **Uptime** — the percentage of time the device was reachable, calculated as `(total heartbeats received / minutes between first and last heartbeat) × 100`, capped at 100 %.
- **Average upload time** — the mean video-upload duration across all reported samples, returned as a human-readable Go duration string (e.g. `1m30s`).

---

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                      main.go                        │
│  • Creates the logger (stdout + timestamped file)   │
│  • Seeds the known device list                      │
│  • Wires routes → middleware → handlers             │
│  • Starts http.ListenAndServe on 127.0.0.1:6733     │
└────────────┬────────────────────────────────────────┘
             │
             ▼
┌────────────────────────┐
│  middleware/logging.go │
│  RequestLogger wraps   │
│  every request:        │
│  • buffers req body    │
│  • captures res body   │
│    and status code     │
│  • emits one log line  │
│    per request         │
└────────────┬───────────┘
             │
             ▼
┌────────────────────────┐      ┌──────────────────────────┐
│  handlers/handlers.go  │─────▶│    store/store.go        │
│  • Heartbeat           │      │  Thread-safe in-memory   │
│  • PostStats           │      │  store protected by a    │
│  • GetStats            │      │  sync.RWMutex.           │
└────────────────────────┘      │                          │
                                │  Per device:             │
                                │  • heartbeatSummary      │
                                │    – firstAt, lastAt,    │
                                │      total               │
                                │  • uploadSummary         │
                                │    – totalTime, count    │
                                └──────────────────────────┘
```

The server has **no external dependencies** — it uses only the Go standard library. All state is held in memory; data does not survive a restart.

---

## API

Base URL: `http://127.0.0.1:6733/api/v1`

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/devices/{device_id}/heartbeat` | Record a heartbeat from a device |
| `POST` | `/devices/{device_id}/stats` | Submit a video-upload time sample |
| `GET`  | `/devices/{device_id}/stats` | Retrieve aggregated device metrics |

### POST `/devices/{device_id}/heartbeat`

**Request body**
```json
{ "sent_at": "2026-05-22T17:00:00Z" }
```

**Responses**

| Status | Meaning |
|--------|---------|
| `204` | Heartbeat recorded |
| `404` | Device ID not registered |
| `500` | Server error |

---

### POST `/devices/{device_id}/stats`

**Request body**
```json
{ "sent_at": "2026-05-22T17:00:00Z", "upload_time": 300000000 }
```

`upload_time` is the number of **nanoseconds** the video upload took.

**Responses**

| Status | Meaning |
|--------|---------|
| `204` | Stat recorded |
| `404` | Device ID not registered |
| `500` | Server error |

---

### GET `/devices/{device_id}/stats`

**Responses**

| Status | Meaning |
|--------|---------|
| `200` | Stats available — returns JSON body (see below) |
| `204` | Device exists but no data has been received yet |
| `404` | Device ID not registered |
| `500` | Server error |

**200 response body**
```json
{
  "avg_upload_time": "5m10s",
  "uptime": 98.5
}
```

- `avg_upload_time` — Go duration string representing the mean upload time across all reported samples.
- `uptime` — uptime percentage (`(total heartbeats / minutes between first and last heartbeat) × 100`), capped at 100.

---

## File Structure

```
fleet-server/
├── go.mod                  # Module declaration (requires Go 1.22+)
├── main.go                 # Entry point: logger setup, device seeding, routing
├── handlers/
│   └── handlers.go         # HTTP request/response handling for all three routes
├── middleware/
│   └── logging.go          # Request-logging middleware (captures req + res)
└── store/
    └── store.go            # Thread-safe in-memory device data store
```

### Key design decisions

- **Go 1.22 standard-library router** — method-prefixed patterns (`"POST /path/{param}"`) and `r.PathValue()` replace the need for a third-party router.
- **Running aggregates** — the store maintains a running total and count for upload times, and first/last/total for heartbeats. No slices are grown over time; reads are O(1) with no iteration.
- **`sync.RWMutex`** — write operations (record heartbeat, record stat) acquire a full write lock; read operations (get stats, has data) acquire a shared read lock, allowing concurrent reads.

---

## Registered Devices

Devices are seeded at startup in `main.go`. Any device ID not in this list will receive a `404` response. To add devices, extend the `seededDevices` slice and restart the server:

```go
var seededDevices = []string{
    "60-6b-44-84-dc-64",
    "b4-45-52-a2-f1-3c",
    "26-9a-66-01-33-83",
    "18-b8-87-e7-1f-06",
    "38-4e-73-e0-33-59",
}
```

---

## Running the Server

```bash
cd fleet-server
go run .
```

The server starts on `http://127.0.0.1:6733`.

### Quick smoke test

```bash
# Heartbeat
curl -s -X POST http://127.0.0.1:6733/api/v1/devices/60-6b-44-84-dc-64/heartbeat \
  -H 'Content-Type: application/json' \
  -d '{"sent_at":"2026-05-22T17:00:00Z"}'

# Upload stat
curl -s -X POST http://127.0.0.1:6733/api/v1/devices/60-6b-44-84-dc-64/stats \
  -H 'Content-Type: application/json' \
  -d '{"sent_at":"2026-05-22T17:00:00Z","upload_time":300000000}'

# Read stats
curl -s http://127.0.0.1:6733/api/v1/devices/60-6b-44-84-dc-64/stats
# → {"avg_upload_time":"300ms","uptime":100}
```

---

## Logging

Every server run creates a new log file under the `logs/` directory (created automatically if it does not exist). The filename encodes the server start time:

```
logs/fleet-server-20260522-170000.log
```

Log output is written to **both the file and stdout** simultaneously via `io.MultiWriter`.

### Log format

Logs use Go's structured `log/slog` text format. Every line contains a timestamp in RFC 3339:

```
time=2026-05-22T17:00:01Z level=INFO msg=request method=POST path=/api/v1/devices/60-6b-44-84-dc-64/heartbeat device_id=60-6b-44-84-dc-64 request_body={"sent_at":"2026-05-22T17:00:00Z"} status=204 response_body= duration_ms=0
```

### Fields logged per request

| Field | Description |
|-------|-------------|
| `time` | RFC 3339 timestamp of when the response was sent |
| `level` | Always `INFO` for request logs |
| `msg` | Always `request` |
| `method` | HTTP method (`GET`, `POST`) |
| `path` | Full request path |
| `device_id` | Device ID extracted from the URL |
| `request_body` | Raw JSON body sent by the caller (empty for `GET`) |
| `status` | HTTP response status code |
| `response_body` | Raw JSON body returned to the caller (empty for `204`) |
| `duration_ms` | Handler wall-clock time in milliseconds |

### Startup log entries

Two `INFO` lines are written before the server begins accepting connections:

```
time=... level=INFO msg="starting Fleet Management server"
time=... level=INFO msg="server ready" addr=http://127.0.0.1:6733 registered_devices=[...] log_file=logs/fleet-server-....log
```

---

## Possible Future Improvements

### Persistence
- Persist device data to a database (e.g. PostgreSQL or SQLite) so metrics survive server restarts.
- Support loading the initial device registry from a configuration file or environment variable rather than a hard-coded slice.

### Device management
- Add a `POST /devices` endpoint to register new devices at runtime without restarting.
- Add a `DELETE /devices/{device_id}` endpoint to deregister devices.
- Return device metadata (registration time, last-seen timestamp) alongside metrics.

### Metrics and observability
- Expose a `/metrics` endpoint in Prometheus format for integration with existing monitoring stacks.
- Add structured per-device uptime history so callers can query uptime over a specific time window rather than the full lifetime window.
- Track and expose the minimum, maximum, and p95 upload times in addition to the mean.

### Reliability
- Add request-body size limits to guard against oversized payloads.
- Add configurable timeouts (read, write, idle) to `http.Server`.
- Implement graceful shutdown on `SIGTERM`/`SIGINT` so in-flight requests complete cleanly.
- Guard against malicious requests for non-registered devices and cut the connection immediately
- Add security so that only authorized reporters can use the APIs

### Testing
- Unit tests for the store's aggregate calculations (uptime formula, average upload time).
- Integration tests that exercise the full HTTP stack against a real `httptest.Server`.

### Configuration
- Move the listen address, log directory, and seeded device list to environment variables or a config file.
- Support JSON-formatted log output (via `slog.NewJSONHandler`) configurable at startup for log-aggregation pipelines.
- Use individual heartbeat frequencies per device
