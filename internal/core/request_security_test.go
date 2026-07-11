package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONEndpointsRequireApplicationJSON(t *testing.T) {
	handler := NewHandler(nil, Config{})
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/metrics"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			request.Header.Set("Content-Type", "text/plain")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("response status = %d, want %d", recorder.Code, http.StatusUnsupportedMediaType)
			}
		})
	}
}

func TestJSONEndpointsRejectTrailingData(t *testing.T) {
	handler := NewHandler(nil, Config{})
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/metrics"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{} {}`))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("response status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestJSONEndpointsRejectOversizedBodies(t *testing.T) {
	handler := NewHandler(nil, Config{})
	oversizedBody := strings.Repeat(" ", int(maxRequestBodyBytes)+1) + `{}`
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/metrics"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(oversizedBody))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("response status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
			}
		})
	}
}

func TestDecodeJSONAcceptsCharsetParameter(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"test"}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	recorder := httptest.NewRecorder()
	var input struct {
		Name string `json:"name"`
	}

	if !decodeJSON(recorder, request, &input) {
		t.Fatalf("decodeJSON rejected valid JSON: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if input.Name != "test" {
		t.Fatalf("decoded name = %q, want test", input.Name)
	}
}
