package store

import (
	"errors"
	"sync"
	"time"
)

// ErrNotFound is returned when a device ID is not registered.
var ErrNotFound = errors.New("device not found")

type heartbeatSummary struct {
	firstAt time.Time
	lastAt  time.Time
	total   int64
}

type uploadSummary struct {
	totalTime time.Duration
	count     int64
}

type deviceData struct {
	hb     heartbeatSummary
	upload uploadSummary
}

// Store is a thread-safe in-memory store for device metrics.
type Store struct {
	mu      sync.RWMutex
	devices map[string]*deviceData
}

// New returns an empty Store.
func New() *Store {
	return &Store{devices: make(map[string]*deviceData)}
}

// RegisterDevice adds a device ID to the set of known devices.
func (s *Store) RegisterDevice(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.devices[id]; !exists {
		s.devices[id] = &deviceData{}
	}
}

// RecordHeartbeat updates the heartbeat summary for the given device.
// On every call it increments the total count and tracks the first and last
// received timestamps.
func (s *Store) RecordHeartbeat(deviceID string, sentAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[deviceID]
	if !ok {
		return ErrNotFound
	}
	d.hb.total++
	if d.hb.total == 1 {
		d.hb.firstAt = sentAt
	}
	if sentAt.After(d.hb.lastAt) {
		d.hb.lastAt = sentAt
	}
	return nil
}

// RecordStat adds an upload-time sample to the running totals for the given device.
func (s *Store) RecordStat(deviceID string, uploadTime time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[deviceID]
	if !ok {
		return ErrNotFound
	}
	d.upload.totalTime += uploadTime
	d.upload.count++
	return nil
}

// DeviceStats holds the computed metrics for a device.
type DeviceStats struct {
	AvgUploadTime time.Duration
	Uptime        float64
}

// HasData returns true if the device has any heartbeats or upload-time samples.
func (s *Store) HasData(deviceID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.devices[deviceID]
	if !ok {
		return false, ErrNotFound
	}
	return d.hb.total > 0 || d.upload.count > 0, nil
}

// GetStats computes and returns metrics for the given device.
func (s *Store) GetStats(deviceID string) (DeviceStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.devices[deviceID]
	if !ok {
		return DeviceStats{}, ErrNotFound
	}

	stats := DeviceStats{}

	if d.upload.count > 0 {
		stats.AvgUploadTime = d.upload.totalTime / time.Duration(d.upload.count)
	}

	if d.hb.total > 0 {
		stats.Uptime = computeUptime(d.hb)
	}

	return stats, nil
}

// computeUptime calculates uptime as a percentage.
//
// Uptime = (totalHeartbeats / minutesBetweenFirstAndLast) * 100
//
// If only one heartbeat has been received (first == last), the window is zero
// and the device is considered fully up (100%).
func computeUptime(hb heartbeatSummary) float64 {
	if hb.total == 0 {
		return 0
	}

	minutes := hb.lastAt.Sub(hb.firstAt).Minutes()
	if minutes <= 0 {
		return 100.0
	}

	uptime := (float64(hb.total) / minutes) * 100.0
	if uptime > 100.0 {
		uptime = 100.0
	}
	return uptime
}
