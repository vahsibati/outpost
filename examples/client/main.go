package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	"github.com/vahsibati/outpost"
)

func main() {
	// 1. Setup a mock server that fails initially, then succeeds later
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		// First 4 requests fail (500 Internal Server Error)
		// Requests 5-7 succeed
		// Requests 8+ fail again
		if count <= 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Server Down"))
			fmt.Printf("[Mock Server] Handled request #%d: Responded with 500 Internal Server Error\n", count)
		} else if count <= 6 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Server OK"))
			fmt.Printf("[Mock Server] Handled request #%d: Responded with 200 OK\n", count)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Server Down"))
			fmt.Printf("[Mock Server] Handled request #%d: Responded with 500 Internal Server Error\n", count)
		}
	}))
	defer server.Close()

	// 2. Setup the Circuit Breaker
	cb := outpost.NewCircuitBreaker(outpost.Settings{
		Name:        "client-breaker",
		MaxRequests: 2,               // 2 successful probes in Half-Open to close the breaker
		Timeout:     2 * time.Second, // 2 seconds timeout in Open state
		ReadyToTrip: func(counts outpost.Counts) bool {
			// Trip if consecutive failures are 3 or more
			return counts.ConsecutiveFailures >= 3
		},
		OnStateChange: func(name string, from outpost.State, to outpost.State) {
			fmt.Printf("\n >>> STATE CHANGE: %s -> %s <<<\n\n", from, to)
		},
	})

	// 3. Wrap a standard HTTP Client
	client := outpost.NewClient(cb, nil)

	fmt.Println("Starting client execution loop against mock server...")
	fmt.Println("Initial breaker state:", cb.State())

	for i := 1; i <= 10; i++ {
		fmt.Printf("--- Client Request #%d ---\n", i)
		resp, err := client.Get(server.URL)
		if err != nil {
			fmt.Printf("[Client Error] Request failed: %v\n", err)
		} else {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			fmt.Printf("[Client Success] Status: %s, Body: %s\n", resp.Status, string(body))
		}

		// Wait 600ms between requests
		time.Sleep(600 * time.Millisecond)
	}
}
