package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mhmdnurf/tarisya/internal/buildinfo"
	"github.com/mhmdnurf/tarisya/internal/config"
	"github.com/mhmdnurf/tarisya/internal/metrics"
)

func TestSend(t *testing.T) {
	var received Payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		if got, want := r.Header.Get("User-Agent"), "tarisya-agent/"+buildinfo.Version; got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := config.Config{
		ServerID: "srv_prod_01",
		APIKey:   "secret",
		Endpoint: server.URL,
		Timeout:  time.Second,
	}
	payload := Payload{
		ServerID:     "srv_prod_01",
		Hostname:     "web-01",
		AgentVersion: buildinfo.Version,
		Timestamp:    time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		Metrics: metrics.Values{
			CPUUsage:      72.5,
			MemoryUsage:   64.2,
			DiskUsage:     48.7,
			LoadAverage:   1.42,
			UptimeSeconds: 864000,
		},
	}

	if err := New(cfg, nil).send(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	if received.ServerID != payload.ServerID {
		t.Fatalf("server_id = %q, want %q", received.ServerID, payload.ServerID)
	}
	if received.AgentVersion != buildinfo.Version {
		t.Fatalf("agent_version = %q, want %q", received.AgentVersion, buildinfo.Version)
	}
	if received.Metrics != payload.Metrics {
		t.Fatalf("metrics = %#v, want %#v", received.Metrics, payload.Metrics)
	}
}

func TestSendRejectsErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid API key", http.StatusUnauthorized)
	}))
	defer server.Close()

	cfg := config.Config{APIKey: "bad", Endpoint: server.URL, Timeout: time.Second}
	if err := New(cfg, nil).send(context.Background(), Payload{}); err == nil {
		t.Fatal("expected an error")
	}
}
