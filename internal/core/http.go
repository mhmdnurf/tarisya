package core

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/mhmdnurf/tarisya/internal/metrics"
)

type MetricsPayload struct {
	ServerID  string         `json:"server_id"`
	Timestamp time.Time      `json:"timestamp"`
	Metrics   metrics.Values `json:"metrics"`
}

type Handler struct {
	store *Store
}

func NewHandler(store *Store) http.Handler {
	h := &Handler{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /api/v1/metrics", h.receiveMetrics)
	return loggingMiddleware(mux)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Healthy(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) receiveMetrics(w http.ResponseWriter, r *http.Request) {
	var payload MetricsPayload
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	if err := validatePayload(payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	apiKey, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
		return
	}
	valid, err := h.store.APIKeyValid(r.Context(), payload.ServerID, apiKey)
	if err != nil {
		slog.Error("could not validate API key", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !valid {
		writeError(w, http.StatusUnauthorized, "invalid API key")
		return
	}

	if err := h.store.SaveMetrics(r.Context(), payload); err != nil {
		slog.Error("could not save metrics", "error", err, "server_id", payload.ServerID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func validatePayload(payload MetricsPayload) error {
	if strings.TrimSpace(payload.ServerID) == "" {
		return errors.New("server_id is required")
	}
	if payload.Timestamp.IsZero() {
		return errors.New("timestamp is required")
	}
	if payload.Timestamp.After(time.Now().Add(5 * time.Minute)) {
		return errors.New("timestamp cannot be in the future")
	}
	values := []float64{
		payload.Metrics.CPUUsage,
		payload.Metrics.MemoryUsage,
		payload.Metrics.DiskUsage,
		payload.Metrics.LoadAverage,
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return errors.New("metric values must be finite and non-negative")
		}
	}
	if payload.Metrics.CPUUsage > 100 ||
		payload.Metrics.MemoryUsage > 100 ||
		payload.Metrics.DiskUsage > 100 {
		return errors.New("usage percentages cannot exceed 100")
	}
	return nil
}

func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(header, " ")
	return token, found && strings.EqualFold(scheme, "Bearer") && token != ""
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("could not write response", "error", err)
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration", time.Since(start),
		)
	})
}
