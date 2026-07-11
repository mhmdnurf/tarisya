package core

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testJWTSecret = "test-secret-that-is-at-least-32-characters"

func TestAPIKeyIsStoredOnlyAsAHash(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	apiKey := "tar_plaintext-key-that-must-never-be-stored"
	if err := store.Bootstrap(ctx, "Test User", "user@example.com", "hash", "srv_test", apiKey); err != nil {
		t.Fatal(err)
	}

	var storedHash string
	if err := store.db.QueryRowContext(ctx, "SELECT api_key_hash FROM server_api_keys WHERE server_id = ?", "srv_test").Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == apiKey {
		t.Fatal("API key was stored in plaintext")
	}
	if want := hashAPIKey(apiKey); storedHash != want {
		t.Fatalf("stored API key hash = %q, want %q", storedHash, want)
	}

	var plaintextRows int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM server_api_keys WHERE api_key_hash = ?", apiKey).Scan(&plaintextRows); err != nil {
		t.Fatal(err)
	}
	if plaintextRows != 0 {
		t.Fatalf("plaintext API key rows = %d, want 0", plaintextRows)
	}
}

func TestRotateAPIKeyRevokesOldKeyAndActivatesReplacement(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	oldKey := "tar_old-key-that-will-be-revoked"
	newKey := "tar_new-key-that-will-remain-active"
	if err := store.Bootstrap(ctx, "Test User", "user@example.com", "hash", "srv_test", oldKey); err != nil {
		t.Fatal(err)
	}
	user, err := store.UserByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatal(err)
	}

	rotated, err := store.RotateAPIKey(ctx, user.ID, "srv_test", newKey)
	if err != nil {
		t.Fatal(err)
	}
	if !rotated {
		t.Fatal("RotateAPIKey returned false for an owned server")
	}
	if valid, err := store.APIKeyValid(ctx, "srv_test", oldKey); err != nil || valid {
		t.Fatalf("old API key valid = %v, error = %v; want false, nil", valid, err)
	}
	if valid, err := store.APIKeyValid(ctx, "srv_test", newKey); err != nil || !valid {
		t.Fatalf("new API key valid = %v, error = %v; want true, nil", valid, err)
	}

	var revoked, active int
	if err := store.db.QueryRowContext(ctx, `SELECT
		SUM(CASE WHEN revoked_at IS NOT NULL THEN 1 ELSE 0 END),
		SUM(CASE WHEN revoked_at IS NULL THEN 1 ELSE 0 END)
		FROM server_api_keys WHERE server_id = ?`, "srv_test").Scan(&revoked, &active); err != nil {
		t.Fatal(err)
	}
	if revoked != 1 || active != 1 {
		t.Fatalf("API key counts revoked=%d active=%d, want 1 and 1", revoked, active)
	}
}

func TestRevokedAPIKeyCannotIngestMetrics(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	oldKey := "tar_old-ingestion-key"
	newKey := "tar_new-ingestion-key"
	if err := store.Bootstrap(ctx, "Test User", "user@example.com", "hash", "srv_test", oldKey); err != nil {
		t.Fatal(err)
	}
	user, err := store.UserByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if rotated, err := store.RotateAPIKey(ctx, user.ID, "srv_test", newKey); err != nil || !rotated {
		t.Fatalf("RotateAPIKey returned %v, %v", rotated, err)
	}

	handler := NewHandler(store, Config{
		JWTSecret:         testJWTSecret,
		JWTExpiration:     time.Hour,
		OfflineThreshold:  time.Minute,
		WarningThreshold:  80,
		CriticalThreshold: 90,
	})
	payload, err := json.Marshal(MetricsPayload{
		ServerID:     "srv_test",
		Hostname:     "test-host",
		AgentVersion: "v1",
		Timestamp:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if status := ingestMetrics(handler, payload, oldKey); status != http.StatusUnauthorized {
		t.Fatalf("revoked key response status = %d, want %d", status, http.StatusUnauthorized)
	}
	if status := ingestMetrics(handler, payload, newKey); status != http.StatusAccepted {
		t.Fatalf("replacement key response status = %d, want %d", status, http.StatusAccepted)
	}

	var metricsCount int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM metrics WHERE server_id = ?", "srv_test").Scan(&metricsCount); err != nil {
		t.Fatal(err)
	}
	if metricsCount != 1 {
		t.Fatalf("stored metrics = %d, want 1", metricsCount)
	}
}

func TestRevokeAPIKeyEndpointIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	apiKey := "tar_key-that-will-be-revoked-without-replacement"
	if err := store.Bootstrap(ctx, "Test User", "user@example.com", "hash", "srv_test", apiKey); err != nil {
		t.Fatal(err)
	}
	user, err := store.UserByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	handler := newAPIKeyTestHandler(store)

	if status := revokeAPIKey(t, handler, user, "srv_test"); status != http.StatusNoContent {
		t.Fatalf("first revocation status = %d, want %d", status, http.StatusNoContent)
	}
	if valid, err := store.APIKeyValid(ctx, "srv_test", apiKey); err != nil || valid {
		t.Fatalf("revoked API key valid = %v, error = %v; want false, nil", valid, err)
	}
	if status := revokeAPIKey(t, handler, user, "srv_test"); status != http.StatusNoContent {
		t.Fatalf("repeated revocation status = %d, want %d", status, http.StatusNoContent)
	}

	var active int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM server_api_keys WHERE server_id=? AND revoked_at IS NULL", "srv_test").Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("active API keys = %d, want 0", active)
	}
}

func TestRevokeAPIKeyEndpointRejectsUnauthorizedUsers(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	apiKey := "tar_key-owned-by-another-user"
	if err := store.Bootstrap(ctx, "Owner", "owner@example.com", "hash", "srv_test", apiKey); err != nil {
		t.Fatal(err)
	}
	otherUser, err := store.CreateUser(ctx, "Other User", "other@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	handler := newAPIKeyTestHandler(store)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/srv_test/revoke-api-key", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous revocation status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if status := revokeAPIKey(t, handler, otherUser, "srv_test"); status != http.StatusNotFound {
		t.Fatalf("unowned server revocation status = %d, want %d", status, http.StatusNotFound)
	}
	if valid, err := store.APIKeyValid(ctx, "srv_test", apiKey); err != nil || !valid {
		t.Fatalf("owner API key valid = %v, error = %v; want true, nil", valid, err)
	}
}

func newAPIKeyTestHandler(store *Store) http.Handler {
	return NewHandler(store, Config{
		JWTSecret:         testJWTSecret,
		JWTExpiration:     time.Hour,
		OfflineThreshold:  time.Minute,
		WarningThreshold:  80,
		CriticalThreshold: 90,
	})
}

func revokeAPIKey(t *testing.T, handler http.Handler, user User, serverID string) int {
	t.Helper()
	token, err := NewAuth(testJWTSecret, time.Hour).Issue(user)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/"+serverID+"/revoke-api-key", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

func ingestMetrics(handler http.Handler, payload []byte, apiKey string) int {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/metrics", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}
