package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mhmdnurf/tarisya/internal/metrics"
)

func TestValidatePayload(t *testing.T) {
	valid := MetricsPayload{
		ServerID:     "srv_01",
		Hostname:     "web-01",
		AgentVersion: "v0.1.0",
		Timestamp:    time.Now(),
		Metrics: metrics.Values{
			CPUUsage:    10,
			MemoryUsage: 20,
			DiskUsage:   30,
			LoadAverage: 1,
		},
	}
	if err := validatePayload(valid); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}

	invalid := valid
	invalid.Metrics.CPUUsage = 101
	if err := validatePayload(invalid); err == nil {
		t.Fatal("usage above 100 should be rejected")
	}
}

func TestBearerToken(t *testing.T) {
	token, ok := bearerToken("Bearer secret")
	if !ok || token != "secret" {
		t.Fatalf("bearerToken returned %q, %v", token, ok)
	}
	if _, ok := bearerToken("secret"); ok {
		t.Fatal("token without scheme should be rejected")
	}
}

func TestHistoryParameters(t *testing.T) {
	request := httptest.NewRequest("GET", "/metrics?limit=25&before=2026-07-09T12:00:00Z", nil)
	limit, before, err := historyParameters(request)
	if err != nil {
		t.Fatal(err)
	}
	if limit != 25 {
		t.Fatalf("limit = %d, want 25", limit)
	}
	if before.IsZero() {
		t.Fatal("before should be parsed")
	}
}

func TestHistoryParametersRejectsInvalidLimit(t *testing.T) {
	request := httptest.NewRequest("GET", "/metrics?limit=501", nil)
	if _, _, err := historyParameters(request); err == nil {
		t.Fatal("expected invalid limit error")
	}
}

func TestChartRange(t *testing.T) {
	duration, bucket, err := chartRange("6h")
	if err != nil {
		t.Fatal(err)
	}
	if duration != 6*time.Hour || bucket != "5 minutes" {
		t.Fatalf("chartRange = %v, %q", duration, bucket)
	}
	if _, _, err := chartRange("7d"); err == nil {
		t.Fatal("unsupported range should be rejected")
	}
}

func TestGeneratedCredentials(t *testing.T) {
	serverID, err := randomHex("srv_", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(serverID) != len("srv_")+20 || !strings.HasPrefix(serverID, "srv_") {
		t.Fatalf("unexpected server ID %q", serverID)
	}
	apiKey, err := randomSecret("tar_", 32)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(apiKey, "tar_") || len(apiKey) < 40 {
		t.Fatalf("unexpected API key format %q", apiKey)
	}
}

func TestOverallStatus(t *testing.T) {
	if got := overallStatus("pending", "unknown"); got != "pending" {
		t.Fatalf("pending overall status = %q", got)
	}
	if got := overallStatus("online", "critical"); got != "critical" {
		t.Fatalf("online critical overall status = %q", got)
	}
}

func TestJWTLifecycle(t *testing.T) {
	auth := NewAuth("a-secret-that-is-longer-than-32-characters", time.Hour)
	user := User{ID: 42, Email: "user@example.com"}
	token, err := auth.Issue(user)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := auth.Parse(token)
	if err != nil {
		t.Fatal(err)
	}
	if userID != user.ID {
		t.Fatalf("user ID = %d, want %d", userID, user.ID)
	}
}

func TestValidateRegistration(t *testing.T) {
	valid := authRequest{Name: "Tarisya User", Email: "user@example.com", Password: "password123"}
	if err := validateRegistration(valid); err != nil {
		t.Fatalf("valid registration rejected: %v", err)
	}

	invalid := valid
	invalid.Password = "short"
	if err := validateRegistration(invalid); err == nil {
		t.Fatal("short password should be rejected")
	}
}

func TestSessionCookieIsHTTPOnly(t *testing.T) {
	handler := &Handler{
		auth:     NewAuth("a-secret-that-is-longer-than-32-characters", time.Hour),
		tokenTTL: time.Hour,
	}
	recorder := httptest.NewRecorder()
	handler.respondWithToken(recorder, User{ID: 1, Email: "user@example.com"}, http.StatusOK)

	response := recorder.Result()
	cookies := response.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count = %d, want 1", len(cookies))
	}
	if !cookies[0].HttpOnly {
		t.Fatal("session cookie must be HttpOnly")
	}
	if strings.Contains(recorder.Body.String(), "access_token") {
		t.Fatal("response body must not expose access token")
	}
}
