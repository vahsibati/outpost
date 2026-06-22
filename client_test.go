package outpost

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_DefaultClassifier(t *testing.T) {
	cb := NewCircuitBreaker(Settings{
		Name: "client-cb",
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures > 1
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("error"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	client := NewClient(cb, nil)

	// Call that succeeds (200 OK)
	resp, err := client.Get(server.URL + "/success")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	_ = resp.Body.Close()
	if cb.Counts().TotalSuccesses != 1 {
		t.Errorf("Expected 1 success, got %d", cb.Counts().TotalSuccesses)
	}

	// Call that fails (500 Internal Server Error)
	resp, err = client.Get(server.URL + "/fail")
	if err != nil {
		t.Fatalf("Expected no error from HTTP layer, got %v", err)
	}
	_ = resp.Body.Close()
	if cb.Counts().ConsecutiveFailures != 1 {
		t.Errorf("Expected 1 consecutive failure, got %d", cb.Counts().ConsecutiveFailures)
	}
	if cb.State() != StateClosed {
		t.Errorf("Expected state to be Closed, got %s", cb.State())
	}

	// Second failing call to trip the breaker
	resp, err = client.Get(server.URL + "/fail")
	if err != nil {
		t.Fatalf("Expected no error from HTTP layer, got %v", err)
	}
	_ = resp.Body.Close()

	if cb.State() != StateOpen {
		t.Errorf("Expected state to transition to Open, got %s", cb.State())
	}

	// Next call should fail fast on the client with ErrOpenState wrapper error
	_, err = client.Get(server.URL + "/success")
	if err == nil {
		t.Fatal("Expected open state error on get, got nil")
	}
}

func TestClient_NetworkErrorFailure(t *testing.T) {
	cb := NewCircuitBreaker(Settings{
		Name: "network-cb",
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures > 1
		},
	})

	client := NewClient(cb, nil)

	// Call a non-existent port to force network/connection failure
	_, err := client.Get("http://localhost:9999/does-not-exist")
	if err == nil {
		t.Fatal("Expected network error, got nil")
	}

	// Since threshold is >1, it shouldn't trip yet, so counts should be 1
	if cb.Counts().ConsecutiveFailures != 1 {
		t.Errorf("Expected consecutive failure count to be 1, got %d", cb.Counts().ConsecutiveFailures)
	}
	if cb.State() != StateClosed {
		t.Errorf("Expected state to remain Closed, got %s", cb.State())
	}

	// Second network failure to trip the breaker
	_, _ = client.Get("http://localhost:9999/does-not-exist")
	if cb.State() != StateOpen {
		t.Errorf("Expected state to transition to Open, got %s", cb.State())
	}
}

func TestClient_CustomClassifier(t *testing.T) {
	cb := NewCircuitBreaker(Settings{
		Name: "custom-cb",
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures > 0
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest) // 400 Bad Request
		_, _ = w.Write([]byte("bad request"))
	}))
	defer server.Close()

	// Configure client with custom classifier: treat 4xx errors as failures too!
	customClassifier := func(resp *http.Response, err error) bool {
		if err != nil {
			return false
		}
		if resp != nil && resp.StatusCode >= 400 {
			return false // Treat 4xx and 5xx as failures
		}
		return true
	}

	client := NewClient(cb, nil, WithResponseClassifier(customClassifier))

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Expected no transport error, got %v", err)
	}
	_ = resp.Body.Close()

	// Should trip because 400 is classified as failure
	if cb.State() != StateOpen {
		t.Errorf("Expected state to be Open, got %s", cb.State())
	}
}

func TestClient_NilClientInitializes(t *testing.T) {
	cb := NewCircuitBreaker(Settings{})
	client := NewClient(cb, nil)
	if client == nil {
		t.Error("NewClient(cb, nil) returned nil client")
	}
	if client.Transport == nil {
		t.Error("NewClient(cb, nil) initialized client with nil Transport")
	}
}

func TestClient_KeepCustomTransportSettings(t *testing.T) {
	cb := NewCircuitBreaker(Settings{})
	customTransport := &http.Transport{
		MaxIdleConns: 42,
	}
	baseClient := &http.Client{
		Transport: customTransport,
		Timeout:   12 * time.Second,
	}

	wrappedClient := NewClient(cb, baseClient)
	rt, ok := wrappedClient.Transport.(*RoundTripper)
	if !ok {
		t.Fatalf("Expected client.Transport to be *RoundTripper, got %T", wrappedClient.Transport)
	}

	if rt.next != customTransport {
		t.Errorf("Expected wrapped transport to be custom transport, got %v", rt.next)
	}
	if rt.next.(*http.Transport).MaxIdleConns != 42 {
		t.Errorf("Expected MaxIdleConns to be 42, got %d", rt.next.(*http.Transport).MaxIdleConns)
	}
	if wrappedClient.Timeout != 12*time.Second {
		t.Errorf("Expected client timeout to remain 12s, got %v", wrappedClient.Timeout)
	}
}
