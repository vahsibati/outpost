package outpost

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddleware_DefaultFlow(t *testing.T) {
	cb := NewCircuitBreaker(Settings{
		Name: "middleware-cb",
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures > 1
		},
	})

	mw := NewMiddleware(cb)
	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("failed"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	// 1. Success request
	req := httptest.NewRequest("GET", "/success", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", rr.Code)
	}
	if cb.Counts().TotalSuccesses != 1 {
		t.Errorf("Expected 1 success count, got %d", cb.Counts().TotalSuccesses)
	}

	// 2. Failure request
	req = httptest.NewRequest("GET", "/fail", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rr.Code)
	}
	if cb.Counts().ConsecutiveFailures != 1 {
		t.Errorf("Expected 1 consecutive failure count, got %d", cb.Counts().ConsecutiveFailures)
	}

	// 3. Second failure request - should trip breaker
	req = httptest.NewRequest("GET", "/fail", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if cb.State() != StateOpen {
		t.Errorf("Expected state to transition to Open, got %s", cb.State())
	}

	// 4. Next request should fail-fast via middleware returning 503
	req = httptest.NewRequest("GET", "/success", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503 Service Unavailable, got %d", rr.Code)
	}
	if rr.Body.String() != "Service Unavailable - Circuit Breaker Open" {
		t.Errorf("Expected default open state body, got %q", rr.Body.String())
	}
}

func TestMiddleware_CustomFallback(t *testing.T) {
	cb := NewCircuitBreaker(Settings{
		Name: "middleware-cb",
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures > 0
		},
	})

	// Setup fallback handler
	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("Custom teapot fallback!"))
	})

	mw := NewMiddleware(cb, WithFallbackHandler(fallback))
	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	// Trip breaker
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if cb.State() != StateOpen {
		t.Fatalf("Expected open state")
	}

	// Call again, should call custom fallback
	req = httptest.NewRequest("GET", "/", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTeapot {
		t.Errorf("Expected custom fallback status 418, got %d", rr.Code)
	}
	if rr.Body.String() != "Custom teapot fallback!" {
		t.Errorf("Expected custom fallback body, got %q", rr.Body.String())
	}
}

func TestMiddleware_CustomStatusClassifier(t *testing.T) {
	cb := NewCircuitBreaker(Settings{
		Name: "middleware-cb",
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures > 0
		},
	})

	// Custom status classifier: treat 404 Not Found as failure
	classifier := func(statusCode int) bool {
		return statusCode != http.StatusNotFound
	}

	mw := NewMiddleware(cb, WithStatusClassifier(classifier))
	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if cb.State() != StateOpen {
		t.Errorf("Expected state to be Open after 404 failure, got %s", cb.State())
	}
}

func TestMiddleware_PanicCapture(t *testing.T) {
	cb := NewCircuitBreaker(Settings{
		Name: "middleware-cb",
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures > 1
		},
	})

	mw := NewMiddleware(cb)
	handler := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went terribly wrong")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	defer func() {
		r := recover()
		if r == nil {
			t.Error("Expected panic to propagate, but it was swallowed")
		}

		// Since threshold is >1, it shouldn't trip yet, so counts should be 1
		if cb.Counts().ConsecutiveFailures != 1 {
			t.Errorf("Expected 1 consecutive failure from panic, got %d", cb.Counts().ConsecutiveFailures)
		}
		if cb.State() != StateClosed {
			t.Errorf("Expected state to remain Closed, got %s", cb.State())
		}
	}()

	handler.ServeHTTP(rr, req)
}
