package outpost

import (
	"net/http"
)

// ResponseClassifier decides if an HTTP response and/or error constitutes a success.
// It returns true for success, false for failure.
type ResponseClassifier func(resp *http.Response, err error) bool

// RoundTripper is an http.RoundTripper wrapper that implements the circuit breaker pattern.
type RoundTripper struct {
	cb                 *CircuitBreaker
	next               http.RoundTripper
	responseClassifier ResponseClassifier
}

// RoundTripperOption allows customization of the RoundTripper.
type RoundTripperOption func(*RoundTripper)

// WithResponseClassifier sets a custom ResponseClassifier for the RoundTripper.
func WithResponseClassifier(classifier ResponseClassifier) RoundTripperOption {
	return func(rt *RoundTripper) {
		rt.responseClassifier = classifier
	}
}

// NewRoundTripper creates a new RoundTripper wrapping next (defaulting to http.DefaultTransport if nil).
func NewRoundTripper(cb *CircuitBreaker, next http.RoundTripper, opts ...RoundTripperOption) *RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}

	rt := &RoundTripper{
		cb:                 cb,
		next:               next,
		responseClassifier: defaultResponseClassifier,
	}

	for _, opt := range opts {
		opt(rt)
	}

	return rt
}

// RoundTrip executes a single HTTP transaction, protecting it with the circuit breaker.
func (rt *RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	generation, err := rt.cb.beforeRequest()
	if err != nil {
		return nil, err
	}

	defer func() {
		if e := recover(); e != nil {
			rt.cb.afterRequest(generation, false)
			panic(e)
		}
	}()

	resp, err := rt.next.RoundTrip(req)
	success := rt.responseClassifier(resp, err)
	rt.cb.afterRequest(generation, success)

	return resp, err
}

// NewClient returns a new http.Client protected by the given circuit breaker.
// If the provided client is nil, it initializes a new one.
func NewClient(cb *CircuitBreaker, client *http.Client, opts ...RoundTripperOption) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	client.Transport = NewRoundTripper(cb, client.Transport, opts...)
	return client
}

func defaultResponseClassifier(resp *http.Response, err error) bool {
	if err != nil {
		return false
	}
	if resp != nil && resp.StatusCode >= 500 {
		return false
	}
	return true
}
