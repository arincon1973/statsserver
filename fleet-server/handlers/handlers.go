package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/safelyyou/fleet-server/store"
)

// Handler holds the dependencies for all HTTP route handlers.
type Handler struct {
	store *store.Store
}

// New returns a Handler backed by the provided store.
func New(s *store.Store) *Handler {
	return &Handler{store: s}
}

// ---- request / response types -----------------------------------------------

type heartbeatRequest struct {
	SentAt time.Time `json:"sent_at"`
}

type uploadStatsRequest struct {
	SentAt     time.Time `json:"sent_at"`
	UploadTime int64     `json:"upload_time"` // nanoseconds
}

type deviceStatsResponse struct {
	AvgUploadTime string  `json:"avg_upload_time"`
	Uptime        float64 `json:"uptime"`
}

type msgResponse struct {
	Msg string `json:"msg"`
}

// ---- helpers ----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusNotFound, msgResponse{Msg: msg})
}

func internalError(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusInternalServerError, msgResponse{Msg: msg})
}

// ---- route handlers ---------------------------------------------------------

// Heartbeat handles POST /api/v1/devices/{device_id}/heartbeat
func (h *Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("device_id")

	var req heartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		internalError(w, "invalid request body: "+err.Error())
		return
	}
	if req.SentAt.IsZero() {
		internalError(w, "sent_at is required")
		return
	}

	if err := h.store.RecordHeartbeat(deviceID, req.SentAt); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "device not found: "+deviceID)
			return
		}
		internalError(w, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PostStats handles POST /api/v1/devices/{device_id}/stats
func (h *Handler) PostStats(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("device_id")

	var req uploadStatsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		internalError(w, "invalid request body: "+err.Error())
		return
	}
	if req.SentAt.IsZero() {
		internalError(w, "sent_at is required")
		return
	}

	uploadDuration := time.Duration(req.UploadTime)
	if err := h.store.RecordStat(deviceID, uploadDuration); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "device not found: "+deviceID)
			return
		}
		internalError(w, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetStats handles GET /api/v1/devices/{device_id}/stats
func (h *Handler) GetStats(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("device_id")

	hasData, err := h.store.HasData(deviceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "device not found: "+deviceID)
			return
		}
		internalError(w, err.Error())
		return
	}
	if !hasData {
		// Device exists but has no data yet.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	stats, err := h.store.GetStats(deviceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "device not found: "+deviceID)
			return
		}
		internalError(w, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, deviceStatsResponse{
		AvgUploadTime: stats.AvgUploadTime.String(),
		Uptime:        stats.Uptime,
	})
}
