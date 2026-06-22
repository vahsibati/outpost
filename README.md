# Outpost 🛡️

Outpost is a lightweight, thread-safe, and highly customizable **Circuit Breaker** library for Go's `net/http` standard package. It is designed to protect both outgoing HTTP calls (via `http.Client` wrapper) and incoming server requests (via `http.Handler` middleware).

---

## Features

- 🔒 **Thread-Safe**: Fully compatible with high-concurrency environments using standard sync primitives.
- 🚦 **Three-State Machine**: Implements standard `Closed`, `Open`, and `Half-Open` states.
- 📡 **HTTP Client Integration**: Wraps `http.RoundTripper` to guard outgoing network requests.
- 🌐 **HTTP Server Middleware**: Protects server endpoints from overload and dependency failures by failing fast.
- ⚙️ **Highly Customizable**: Custom response/status classifiers, failure rate/consecutive threshold rules, state transition callbacks, and fallback handlers.

---

## Installation

```bash
go get github.com/vahsibati/outpost
```

---

## State Machine

```
              +-------------------------+
              |                         | (Failure threshold exceeded)
              v                         |
          +--------+                +------+
  ======> | Closed | -------------> | Open |
          +--------+                +------+
              ^                         |
              | (Max successes          | (Timeout elapsed)
              |  reached)               v
              |                   +-----------+
              +------------------ | Half-Open |
                                  +-----------+
                                        |
                                        | (Any probe failure)
                                        v
                                    (Back to Open)
```

---

## Quick Start

### 1. Define a Circuit Breaker

```go
import (
    "time"
    "github.com/vahsibati/outpost"
)

cb := outpost.NewCircuitBreaker(outpost.Settings{
    Name:        "my-service-breaker",
    MaxRequests: 3,               // Number of successful requests needed in Half-Open to close the breaker
    Timeout:     5 * time.Second, // Duration to stay in Open state before transitioning to Half-Open
    ReadyToTrip: func(counts outpost.Counts) bool {
        // Trip the breaker if consecutive failures reach 5
        return counts.ConsecutiveFailures >= 5
    },
    OnStateChange: func(name string, from outpost.State, to outpost.State) {
        println("Breaker state changed from", from.String(), "to", to.String())
    },
})
```

### 2. HTTP Client Protection

Wrap an existing `http.Client` to protect outgoing calls. By default, network transport errors and HTTP status codes `>= 500` are classified as failures.

```go
import (
    "net/http"
    "github.com/vahsibati/outpost"
)

// Wrap any existing http.Client
client := outpost.NewClient(cb, &http.Client{Timeout: 10 * time.Second})

// Make requests as usual
resp, err := client.Get("https://api.unstable-service.com/data")
```

#### Custom Response Classification:
Customize what is considered a failure (e.g., classifying `4xx` status codes as failures as well):

```go
client := outpost.NewClient(cb, nil, outpost.WithResponseClassifier(func(resp *http.Response, err error) bool {
    if err != nil {
        return false // Network error = failure
    }
    if resp != nil && resp.StatusCode >= 400 {
        return false // 4xx and 5xx = failure
    }
    return true // success
}))
```

### 3. HTTP Server Middleware Protection

Wrap server handlers to protect them from cascading failures or downstream dependency down-time.

```go
import (
    "net/http"
    "github.com/vahsibati/outpost"
)

// Create middleware
mw := outpost.NewMiddleware(cb)

// Wrap your HTTP router or handler
mux := http.NewServeMux()
mux.Handle("/heavy-task", mw.Handler(http.HandlerFunc(myHandler)))

http.ListenAndServe(":8080", mux)
```

#### Custom Fallback Handler:
Provide custom JSON or HTML responses instead of the default plain-text `503 Service Unavailable` message when the breaker is open:

```go
fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusServiceUnavailable)
    w.Write([]byte(`{"error": "service temporarily unavailable, please try again later"}`))
})

mw := outpost.NewMiddleware(cb, outpost.WithFallbackHandler(fallback))
```

---

## Configuration Settings

| Parameter | Type | Description |
| :--- | :--- | :--- |
| `Name` | `string` | Name of the circuit breaker. Used for logs or callbacks. |
| `MaxRequests` | `uint32` | Number of successful probe requests needed in Half-Open state to close the breaker. (Default: 1) |
| `Interval` | `time.Duration` | Sliding window interval to reset statistics in Closed state. If 0, stats are never automatically cleared. |
| `Timeout` | `time.Duration` | Period to wait in Open state before transitioning to Half-Open. (Default: 60s) |
| `ReadyToTrip` | `func(Counts) bool` | Custom function evaluated after failures to decide whether to trip the breaker. (Default: >5 consecutive failures) |
| `OnStateChange` | `func(name, from, to)` | Callback triggered whenever the breaker transitions states. |

---

## License

MIT License.
