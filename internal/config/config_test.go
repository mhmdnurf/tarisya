package config

import "testing"

func TestMetricsEndpoint(t *testing.T) {
	for _, coreURL := range []string{"http://localhost:8081", "http://localhost:8081/"} {
		got, err := metricsEndpoint(coreURL)
		if err != nil {
			t.Fatal(err)
		}
		if want := "http://localhost:8081/api/v1/metrics"; got != want {
			t.Fatalf("metricsEndpoint(%q) = %q, want %q", coreURL, got, want)
		}
	}
}

func TestMetricsEndpointRejectsRelativeURL(t *testing.T) {
	if _, err := metricsEndpoint("localhost:8081"); err == nil {
		t.Fatal("relative URL should be rejected")
	}
}
