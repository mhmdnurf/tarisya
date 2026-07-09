package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/mhmdnurf/tarisya/internal/config"
	"github.com/mhmdnurf/tarisya/internal/metrics"
)

type Payload struct {
	ServerID     string         `json:"server_id"`
	Hostname     string         `json:"hostname"`
	AgentVersion string         `json:"agent_version"`
	Timestamp    time.Time      `json:"timestamp"`
	Metrics      metrics.Values `json:"metrics"`
}

const Version = "v0.1.0"

type Agent struct {
	config    config.Config
	collector metrics.Collector
	client    *http.Client
	hostname  string
}

func New(cfg config.Config, collector metrics.Collector) *Agent {
	if collector == nil {
		collector = metrics.SystemCollector{}
	}
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
	}
	return &Agent{
		config:    cfg,
		collector: collector,
		client:    &http.Client{Timeout: cfg.Timeout},
		hostname:  hostname,
	}
}

func (a *Agent) Run(ctx context.Context) error {
	// Send immediately on startup, then continue at the configured interval.
	a.collectAndSend(ctx)

	ticker := time.NewTicker(a.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			a.collectAndSend(ctx)
		}
	}
}

func (a *Agent) collectAndSend(ctx context.Context) {
	values, err := a.collector.Collect(ctx, a.config.DiskPath)
	if err != nil {
		slog.Error("could not collect metrics", "error", err)
		return
	}

	payload := Payload{
		ServerID:     a.config.ServerID,
		Hostname:     a.hostname,
		AgentVersion: Version,
		Timestamp:    time.Now().UTC(),
		Metrics:      values,
	}
	if err := a.send(ctx, payload); err != nil {
		slog.Error("could not send metrics", "error", err)
		return
	}

	slog.Info("metrics sent",
		"cpu_usage", values.CPUUsage,
		"memory_usage", values.MemoryUsage,
		"disk_usage", values.DiskUsage,
	)
}

func (a *Agent) send(ctx context.Context, payload Payload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.config.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tarisya-agent/0.1")

	response, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("request Tarisya Core: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Tarisya Core returned %s: %s", response.Status, string(message))
	}
	return nil
}
