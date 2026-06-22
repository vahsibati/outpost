package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/vahsibati/outpost"
)

func main() {
	// Initialize a circuit breaker
	cb := outpost.NewCircuitBreaker(outpost.Settings{
		Name:        "server-breaker",
		MaxRequests: 3,               // 3 successful probes to close again
		Timeout:     5 * time.Second, // wait 5s in open before going half-open
		ReadyToTrip: func(counts outpost.Counts) bool {
			// Trip if consecutive failures are greater than 3
			return counts.ConsecutiveFailures >= 3
		},
		OnStateChange: func(name string, from outpost.State, to outpost.State) {
			log.Printf("[Outpost State Change] %s: %s -> %s\n", name, from, to)
		},
	})

	// Wrap middleware
	mw := outpost.NewMiddleware(cb)

	// Route 1: Unstable endpoint (fails 50% of the time)
	http.Handle("/unstable", mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Can manually trigger failure using ?fail=true
		failParam := r.URL.Query().Get("fail")
		shouldFail := failParam == "true" || (failParam == "" && rand.Float32() < 0.5)

		if shouldFail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Server Error: Something failed inside!\n"))
			log.Println("[Server] Request failed internally.")
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Success: Everything is working fine!\n"))
		log.Println("[Server] Request completed successfully.")
	})))

	// Route 2: Status page (shows state of the circuit breaker)
	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		counts := cb.Counts()
		statusData := map[string]interface{}{
			"name":                 cb.Name(),
			"state":                cb.State().String(),
			"requests":             counts.Requests,
			"total_successes":      counts.TotalSuccesses,
			"total_failures":       counts.TotalFailures,
			"consecutive_success":  counts.ConsecutiveSuccesses,
			"consecutive_failures": counts.ConsecutiveFailures,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(statusData)
	})

	fmt.Println("Server is starting on :8080...")
	fmt.Println("Endpoints:")
	fmt.Println("  GET http://localhost:8080/unstable        (Protected endpoint, fails occasionally)")
	fmt.Println("  GET http://localhost:8080/unstable?fail=true (Force failure)")
	fmt.Println("  GET http://localhost:8080/status          (Retrieve circuit breaker status)")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
