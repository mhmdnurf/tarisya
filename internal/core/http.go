package core

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/mhmdnurf/tarisya/internal/metrics"
)

type MetricsPayload struct {
	ServerID     string         `json:"server_id"`
	Hostname     string         `json:"hostname"`
	AgentVersion string         `json:"agent_version"`
	Timestamp    time.Time      `json:"timestamp"`
	Metrics      metrics.Values `json:"metrics"`
}

type Handler struct {
	store              *Store
	auth               *Auth
	allowedOrigins     map[string]struct{}
	tokenTTL           time.Duration
	cookieSecure       bool
	offlineThreshold   time.Duration
	warningThreshold   float64
	criticalThreshold  float64
	publicCoreURL      string
	authRateLimiter    *clientRateLimiter
	metricsRateLimiter *clientRateLimiter
	actionRateLimiter  *clientRateLimiter
}

const (
	sessionCookieName   = "tarisya_session"
	maxRequestBodyBytes = int64(64 << 10)
)

type authRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type createServerRequest struct {
	Name string `json:"name"`
}

func NewHandler(store *Store, cfg Config) http.Handler {
	origins := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, origin := range cfg.AllowedOrigins {
		origins[origin] = struct{}{}
	}
	h := &Handler{
		store:             store,
		auth:              NewAuth(cfg.JWTSecret, cfg.JWTExpiration),
		allowedOrigins:    origins,
		tokenTTL:          cfg.JWTExpiration,
		cookieSecure:      cfg.CookieSecure,
		offlineThreshold:  cfg.OfflineThreshold,
		warningThreshold:  cfg.WarningThreshold,
		criticalThreshold: cfg.CriticalThreshold,
		publicCoreURL:     cfg.PublicCoreURL,
		authRateLimiter: newClientRateLimiter(
			configuredRateLimit(cfg.AuthRateLimitPerMinute, defaultAuthRateLimitPerMinute),
			configuredRateLimit(cfg.AuthRateLimitBurst, defaultAuthRateLimitBurst),
		),
		metricsRateLimiter: newClientRateLimiter(
			configuredRateLimit(cfg.MetricsRateLimitPerMinute, defaultMetricsRateLimitPerMinute),
			configuredRateLimit(cfg.MetricsRateLimitBurst, defaultMetricsRateLimitBurst),
		),
		actionRateLimiter: newClientRateLimiter(
			configuredRateLimit(cfg.ActionRateLimitPerMinute, defaultActionRateLimitPerMinute),
			configuredRateLimit(cfg.ActionRateLimitBurst, defaultActionRateLimitBurst),
		),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.Handle("POST /api/v1/auth/register", h.rateLimit(h.authRateLimiter, h.register))
	mux.Handle("POST /api/v1/auth/login", h.rateLimit(h.authRateLimiter, h.login))
	mux.HandleFunc("POST /api/v1/auth/logout", h.logout)
	mux.HandleFunc("GET /api/v1/auth/me", h.me)
	mux.Handle("POST /api/v1/metrics", h.rateLimit(h.metricsRateLimiter, h.receiveMetrics))
	mux.Handle("POST /api/v1/servers", h.rateLimit(h.actionRateLimiter, h.createServer))
	mux.HandleFunc("GET /api/v1/servers", h.listServers)
	mux.HandleFunc("GET /api/v1/servers/{id}", h.serverDetail)
	mux.Handle("DELETE /api/v1/servers/{id}", h.rateLimit(h.actionRateLimiter, h.deleteServer))
	mux.Handle("POST /api/v1/servers/{id}/rotate-api-key", h.rateLimit(h.actionRateLimiter, h.rotateServerAPIKey))
	mux.Handle("POST /api/v1/servers/{id}/revoke-api-key", h.rateLimit(h.actionRateLimiter, h.revokeServerAPIKey))
	mux.HandleFunc("GET /api/v1/servers/{id}/latest-metrics", h.latestMetrics)
	mux.HandleFunc("GET /api/v1/servers/{id}/metrics", h.metricsHistory)
	return loggingMiddleware(h.corsMiddleware(mux))
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Healthy(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var input authRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if err := validateRegistration(input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	passwordHash, err := HashPassword(input.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	user, err := h.store.CreateUser(r.Context(), input.Name, input.Email, passwordHash)
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "email is already registered")
		return
	}
	if err != nil {
		slog.Error("could not register user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.respondWithToken(w, user, http.StatusCreated)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var input authRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	user, err := h.store.UserByEmail(r.Context(), input.Email)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		slog.Error("could not find user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	matches, needsRehash := verifyPassword(user.PasswordHash, input.Password)
	if !matches {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if needsRehash {
		newHash, hashErr := HashPassword(input.Password)
		if hashErr != nil {
			slog.Error("could not upgrade password hash", "error", hashErr, "user_id", user.ID)
		} else if updateErr := h.store.UpdatePasswordHash(r.Context(), user.ID, user.PasswordHash, newHash); updateErr != nil {
			slog.Error("could not store upgraded password hash", "error", updateErr, "user_id", user.ID)
		}
	}
	h.respondWithToken(w, user, http.StatusOK)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticateUser(w, r)
	if !ok {
		return
	}
	user, err := h.store.UserByID(r.Context(), userID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "user no longer exists")
		return
	}
	if err != nil {
		slog.Error("could not find current user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": user})
}

func (h *Handler) logout(w http.ResponseWriter, _ *http.Request) {
	h.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (h *Handler) listServers(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticateUser(w, r)
	if !ok {
		return
	}
	servers, err := h.store.ServersByUser(
		r.Context(),
		userID,
		h.offlineThreshold,
		h.warningThreshold,
		h.criticalThreshold,
	)
	if err != nil {
		slog.Error("could not list servers", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": servers})
}

func (h *Handler) createServer(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticateUser(w, r)
	if !ok {
		return
	}
	var input createServerRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if len(input.Name) < 2 || len(input.Name) > 100 {
		writeError(w, http.StatusBadRequest, "name must contain 2 to 100 characters")
		return
	}

	serverID, err := randomHex("srv_", 10)
	if err != nil {
		slog.Error("could not generate server ID", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	apiKey, err := randomSecret("tar_", 32)
	if err != nil {
		slog.Error("could not generate API key", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	server, err := h.store.CreateServer(r.Context(), userID, serverID, input.Name, apiKey)
	if err != nil {
		slog.Error("could not create server", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"data": map[string]any{
			"server": server,
			"agent_config": map[string]string{
				"server_id": server.ID,
				"api_key":   apiKey,
				"core_url":  h.publicCoreURL,
			},
		},
	})
}

func (h *Handler) deleteServer(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticateUser(w, r)
	if !ok {
		return
	}
	deleted, err := h.store.DeleteServer(r.Context(), userID, r.PathValue("id"))
	if err != nil {
		slog.Error("could not delete server", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) rotateServerAPIKey(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticateUser(w, r)
	if !ok {
		return
	}
	apiKey, err := randomSecret("tar_", 32)
	if err != nil {
		slog.Error("could not generate API key", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	rotated, err := h.store.RotateAPIKey(r.Context(), userID, r.PathValue("id"), apiKey)
	if err != nil {
		slog.Error("could not rotate API key", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !rotated {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]string{
			"server_id": r.PathValue("id"),
			"api_key":   apiKey,
			"core_url":  h.publicCoreURL,
		},
	})
}

func (h *Handler) revokeServerAPIKey(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticateUser(w, r)
	if !ok {
		return
	}
	revoked, err := h.store.RevokeAPIKey(r.Context(), userID, r.PathValue("id"))
	if err != nil {
		slog.Error("could not revoke API key", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !revoked {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) serverDetail(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticateUser(w, r)
	if !ok {
		return
	}
	server, err := h.store.ServerByUser(
		r.Context(),
		userID,
		r.PathValue("id"),
		h.offlineThreshold,
		h.warningThreshold,
		h.criticalThreshold,
	)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	if err != nil {
		slog.Error("could not read server", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": server})
}

func (h *Handler) receiveMetrics(w http.ResponseWriter, r *http.Request) {
	var payload MetricsPayload
	if !decodeJSON(w, r, &payload) {
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
		if errors.Is(err, ErrDatabaseFull) {
			writeError(w, http.StatusInsufficientStorage, "metric ingestion is paused because the database size limit was reached")
			return
		}
		slog.Error("could not save metrics", "error", err, "server_id", payload.ServerID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *Handler) latestMetrics(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("id")
	if !h.authorizeUserServer(w, r, serverID) {
		return
	}

	record, err := h.store.LatestMetrics(r.Context(), serverID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "metrics not found")
		return
	}
	if err != nil {
		slog.Error("could not read latest metrics", "error", err, "server_id", serverID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": record})
}

func (h *Handler) metricsHistory(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("id")
	if !h.authorizeUserServer(w, r, serverID) {
		return
	}

	if value := r.URL.Query().Get("range"); value != "" {
		h.metricsChart(w, r, serverID, value)
		return
	}

	limit, before, err := historyParameters(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	records, err := h.store.MetricsHistory(r.Context(), serverID, limit+1, before)
	if err != nil {
		slog.Error("could not read metrics history", "error", err, "server_id", serverID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	var nextCursor string
	if hasMore && len(records) > 0 {
		nextCursor = records[len(records)-1].CollectedAt.UTC().Format(time.RFC3339Nano)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": records,
		"pagination": map[string]any{
			"limit":       limit,
			"has_more":    hasMore,
			"next_cursor": nextCursor,
		},
	})
}

func (h *Handler) metricsChart(w http.ResponseWriter, r *http.Request, serverID, value string) {
	duration, bucket, err := chartRange(value)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	end := time.Now().UTC()
	start := end.Add(-duration)
	records, statistics, err := h.store.MetricsChart(
		r.Context(),
		serverID,
		start,
		end,
		bucket,
	)
	if err != nil {
		slog.Error("could not read chart metrics", "error", err, "server_id", serverID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":       records,
		"statistics": statistics,
		"range": map[string]any{
			"value":  value,
			"start":  start,
			"end":    end,
			"bucket": bucket,
		},
	})
}

func chartRange(value string) (time.Duration, string, error) {
	switch value {
	case "15m":
		return 15 * time.Minute, "15 seconds", nil
	case "1h":
		return time.Hour, "1 minute", nil
	case "6h":
		return 6 * time.Hour, "5 minutes", nil
	case "24h":
		return 24 * time.Hour, "15 minutes", nil
	default:
		return 0, "", errors.New("range must be one of: 15m, 1h, 6h, 24h")
	}
}

func (h *Handler) authenticateUser(w http.ResponseWriter, r *http.Request) (int64, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusUnauthorized, "missing session")
		return 0, false
	}
	userID, err := h.auth.Parse(cookie.Value)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return 0, false
	}
	return userID, true
}

func (h *Handler) authorizeUserServer(w http.ResponseWriter, r *http.Request, serverID string) bool {
	userID, ok := h.authenticateUser(w, r)
	if !ok {
		return false
	}
	owns, err := h.store.UserOwnsServer(r.Context(), userID, serverID)
	if err != nil {
		slog.Error("could not verify server ownership", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return false
	}
	if !owns {
		writeError(w, http.StatusNotFound, "server not found")
		return false
	}
	return true
}

func (h *Handler) respondWithToken(w http.ResponseWriter, user User, status int) {
	token, err := h.auth.Issue(user)
	if err != nil {
		slog.Error("could not issue access token", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.setSessionCookie(w, token)
	writeJSON(w, status, map[string]any{
		"data": map[string]any{
			"expires_in": int64(h.tokenTTL.Seconds()),
			"user":       user,
		},
	})
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(h.tokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func validateRegistration(input authRequest) error {
	if len(input.Name) < 2 || len(input.Name) > 100 {
		return errors.New("name must contain 2 to 100 characters")
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || !strings.EqualFold(address.Address, input.Email) {
		return errors.New("email must be valid")
	}
	if len(input.Password) < 8 || len(input.Password) > 128 {
		return errors.New("password must contain 8 to 128 characters")
	}
	return nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSONDecodeError(w, err)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSONDecodeError(w, err)
		return false
	}
	return true
}

func writeJSONDecodeError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "request body exceeds 64 KiB limit")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid JSON payload")
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func historyParameters(r *http.Request) (int, time.Time, error) {
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 500 {
			return 0, time.Time{}, errors.New("limit must be between 1 and 500")
		}
		limit = parsed
	}

	var before time.Time
	if value := r.URL.Query().Get("before"); value != "" {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return 0, time.Time{}, errors.New("before must use RFC3339 format")
		}
		before = parsed
	}
	return limit, before, nil
}

func validatePayload(payload MetricsPayload) error {
	if strings.TrimSpace(payload.ServerID) == "" {
		return errors.New("server_id is required")
	}
	if strings.TrimSpace(payload.Hostname) == "" || len(payload.Hostname) > 255 {
		return errors.New("hostname must contain 1 to 255 characters")
	}
	if strings.TrimSpace(payload.AgentVersion) == "" || len(payload.AgentVersion) > 50 {
		return errors.New("agent_version must contain 1 to 50 characters")
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

func randomHex(prefix string, byteLength int) (string, error) {
	value := make([]byte, byteLength)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value), nil
}

func randomSecret(prefix string, byteLength int) (string, error) {
	value := make([]byte, byteLength)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(value), nil
}

func (h *Handler) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if _, allowed := h.allowedOrigins[origin]; origin != "" && allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			if _, allowed := h.allowedOrigins[origin]; !allowed {
				writeError(w, http.StatusForbidden, "origin is not allowed")
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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
