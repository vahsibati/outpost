package outpost

import (
	"net/http"
)

// StatusClassifier decides if an HTTP status code constitutes a success.
// It returns true for success, false for failure.
type StatusClassifier func(statusCode int) bool

// Middleware wraps http.Handlers with circuit breaker logic.
type Middleware struct {
	cb               *CircuitBreaker
	fallbackHandler  http.Handler
	statusClassifier StatusClassifier
}

// MiddlewareOption allows customization of the Middleware.
type MiddlewareOption func(*Middleware)

// WithFallbackHandler sets a custom handler to be invoked when the breaker is open.
func WithFallbackHandler(h http.Handler) MiddlewareOption {
	return func(m *Middleware) {
		m.fallbackHandler = h
	}
}

// WithStatusClassifier sets a custom StatusClassifier to determine success based on HTTP status code.
func WithStatusClassifier(classifier StatusClassifier) MiddlewareOption {
	return func(m *Middleware) {
		m.statusClassifier = classifier
	}
}

// NewMiddleware creates a new Middleware instance with the provided options.
func NewMiddleware(cb *CircuitBreaker, opts ...MiddlewareOption) *Middleware {
	m := &Middleware{
		cb:               cb,
		statusClassifier: defaultStatusClassifier,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// Handler wraps the given http.Handler with the circuit breaker protection.
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		generation, err := m.cb.beforeRequest()
		if err != nil {
			if m.fallbackHandler != nil {
				m.fallbackHandler.ServeHTTP(w, r)
			} else {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("Service Unavailable - Circuit Breaker Open"))
			}
			return
		}

		wrapper := &responseWriterWrapper{ResponseWriter: w}

		defer func() {
			if e := recover(); e != nil {
				m.cb.afterRequest(generation, false)
				panic(e) // re-panic
			} else {
				statusCode := wrapper.statusCode
				if statusCode == 0 {
					statusCode = http.StatusOK
				}
				success := m.statusClassifier(statusCode)
				m.cb.afterRequest(generation, success)
			}
		}()

		next.ServeHTTP(wrapper, r)
	})
}

func defaultStatusClassifier(statusCode int) bool {
	// 5xx status codes are treated as failures, anything else is a success
	return statusCode < 500
}

type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriterWrapper) Write(b []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Flush implements the http.Flusher interface if the underlying ResponseWriter supports it.
func (w *responseWriterWrapper) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
