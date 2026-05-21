package main

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/safelyyou/fleet-server/handlers"
	"github.com/safelyyou/fleet-server/middleware"
	"github.com/safelyyou/fleet-server/store"
)

// seededDevices lists the device IDs that are considered registered on startup.
// The heartbeat and stats endpoints return 404 for any ID not in this list.
var seededDevices = []string{
	"60-6b-44-84-dc-64",
	"b4-45-52-a2-f1-3c",
	"26-9a-66-01-33-83",
	"18-b8-87-e7-1f-06",
	"38-4e-73-e0-33-59",
}

// newLogger creates a slog.Logger that writes structured text logs to both
// stdout and a timestamped file under the "logs/" directory.
// The file is named: fleet-server-YYYYMMDD-HHMMSS.log
func newLogger() (*slog.Logger, *os.File, error) {
	if err := os.MkdirAll("logs", 0o755); err != nil {
		return nil, nil, fmt.Errorf("create logs dir: %w", err)
	}

	filename := fmt.Sprintf("logs/fleet-server-%s.log", time.Now().Format("20060102-150405"))
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}

	w := io.MultiWriter(os.Stdout, f)
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		// Include full timestamps in every log line.
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(a.Value.Time().Format(time.RFC3339))
			}
			return a
		},
	})
	return slog.New(handler), f, nil
}

func main() {
	logger, logFile, err := newLogger()
	if err != nil {
		log.Fatalf("init logger: %v", err)
	}
	defer logFile.Close()

	logger.Info("starting Fleet Management server")

	s := store.New()
	for _, id := range seededDevices {
		s.RegisterDevice(id)
	}

	h := handlers.New(s)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/devices/{device_id}/heartbeat", h.Heartbeat)
	mux.HandleFunc("POST /api/v1/devices/{device_id}/stats", h.PostStats)
	mux.HandleFunc("GET /api/v1/devices/{device_id}/stats", h.GetStats)

	addr := "127.0.0.1:6733"
	logger.Info("server ready",
		"addr", "http://"+addr,
		"registered_devices", seededDevices,
		"log_file", logFile.Name(),
	)

	if err := http.ListenAndServe(addr, middleware.RequestLogger(logger, mux)); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}
