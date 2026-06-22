package outpost

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb := NewCircuitBreaker(Settings{
		Name: "test-cb",
	})

	if cb.State() != StateClosed {
		t.Errorf("Expected initial state to be Closed, got %s", cb.State())
	}
	counts := cb.Counts()
	if counts.Requests != 0 {
		t.Errorf("Expected requests to be 0, got %d", counts.Requests)
	}
}

func TestCircuitBreaker_TripToOpen(t *testing.T) {
	cb := NewCircuitBreaker(Settings{
		Name: "test-cb",
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures > 2
		},
	})

	// First failure
	_, err := cb.Execute(func() (interface{}, error) {
		return nil, errors.New("fail")
	})
	if err == nil || err.Error() != "fail" {
		t.Errorf("Expected failure error, got %v", err)
	}
	if cb.State() != StateClosed {
		t.Errorf("Expected state to be Closed, got %s", cb.State())
	}

	// Second failure
	_, _ = cb.Execute(func() (interface{}, error) {
		return nil, errors.New("fail")
	})
	if cb.State() != StateClosed {
		t.Errorf("Expected state to be Closed, got %s", cb.State())
	}

	// Third failure - should trip
	_, _ = cb.Execute(func() (interface{}, error) {
		return nil, errors.New("fail")
	})
	if cb.State() != StateOpen {
		t.Errorf("Expected state to be Open, got %s", cb.State())
	}

	// Subsequent execution should fail fast
	_, err = cb.Execute(func() (interface{}, error) {
		return "success", nil
	})
	if err != ErrOpenState {
		t.Errorf("Expected ErrOpenState, got %v", err)
	}
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	cb := NewCircuitBreaker(Settings{
		Name:        "test-cb",
		Timeout:     10 * time.Millisecond,
		MaxRequests: 2,
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures > 1
		},
	})

	// Trip it
	_, _ = cb.Execute(func() (interface{}, error) { return nil, errors.New("fail") })
	_, _ = cb.Execute(func() (interface{}, error) { return nil, errors.New("fail") })

	if cb.State() != StateOpen {
		t.Fatalf("Expected open state, got %v", cb.State())
	}

	// Wait for timeout to expire
	time.Sleep(15 * time.Millisecond)

	// Next call should transition state to Half-Open and allow the call
	res, err := cb.Execute(func() (interface{}, error) {
		return "ok", nil
	})
	if err != nil || res != "ok" {
		t.Errorf("Expected success, got res=%v, err=%v", res, err)
	}
	if cb.State() != StateHalfOpen {
		t.Errorf("Expected state to be HalfOpen, got %s", cb.State())
	}

	// Second probe call to recover
	_, _ = cb.Execute(func() (interface{}, error) {
		return "ok", nil
	})

	// With MaxRequests = 2 and 2 successes, it should transition back to Closed
	if cb.State() != StateClosed {
		t.Errorf("Expected state to be Closed after successful probes, got %s", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenFailBackToOpen(t *testing.T) {
	cb := NewCircuitBreaker(Settings{
		Name:        "test-cb",
		Timeout:     10 * time.Millisecond,
		MaxRequests: 2,
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures > 0
		},
	})

	// Trip
	_, _ = cb.Execute(func() (interface{}, error) { return nil, errors.New("fail") })
	if cb.State() != StateOpen {
		t.Fatalf("Expected open state")
	}

	time.Sleep(15 * time.Millisecond)

	// Probe fails, should transition straight back to Open
	_, err := cb.Execute(func() (interface{}, error) {
		return nil, errors.New("probe fail")
	})
	if err == nil || err.Error() != "probe fail" {
		t.Errorf("Expected probe fail error, got %v", err)
	}
	if cb.State() != StateOpen {
		t.Errorf("Expected state to transition back to Open, got %s", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenLimit(t *testing.T) {
	cb := NewCircuitBreaker(Settings{
		Name:        "test-cb",
		Timeout:     10 * time.Millisecond,
		MaxRequests: 1,
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures > 0
		},
	})

	// Trip
	_, _ = cb.Execute(func() (interface{}, error) { return nil, errors.New("fail") })
	time.Sleep(15 * time.Millisecond)

	// Start a probe but block it to hold HalfOpen state with max request reached
	var wg sync.WaitGroup
	wg.Add(1)
	unblockProbe := make(chan struct{})

	go func() {
		defer wg.Done()
		_, _ = cb.Execute(func() (interface{}, error) {
			<-unblockProbe
			return "ok", nil
		})
	}()

	// Wait briefly to make sure the goroutine started and entered execute
	time.Sleep(2 * time.Millisecond)

	// Since MaxRequests = 1 and one is in progress, any other call should fail-fast with ErrTooManyRequests
	_, err := cb.Execute(func() (interface{}, error) {
		return "ignored", nil
	})
	if err != ErrTooManyRequests {
		t.Errorf("Expected ErrTooManyRequests, got %v", err)
	}

	// Unblock and wait
	close(unblockProbe)
	wg.Wait()

	// Breaker should be Closed now since the probe succeeded
	if cb.State() != StateClosed {
		t.Errorf("Expected state to be Closed, got %s", cb.State())
	}
}

func TestCircuitBreaker_IntervalReset(t *testing.T) {
	cb := NewCircuitBreaker(Settings{
		Name:     "test-cb",
		Interval: 10 * time.Millisecond,
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures > 2
		},
	})

	_, _ = cb.Execute(func() (interface{}, error) { return nil, errors.New("fail") })
	_, _ = cb.Execute(func() (interface{}, error) { return nil, errors.New("fail") })

	if cb.Counts().ConsecutiveFailures != 2 {
		t.Fatalf("Expected 2 consecutive failures, got %d", cb.Counts().ConsecutiveFailures)
	}

	// Wait for interval to elapse
	time.Sleep(15 * time.Millisecond)

	// Executing a new call should trigger a generation change and clear statistics
	_, _ = cb.Execute(func() (interface{}, error) { return "ok", nil })

	if cb.Counts().ConsecutiveFailures != 0 {
		t.Errorf("Expected consecutive failures to reset to 0 after interval, got %d", cb.Counts().ConsecutiveFailures)
	}
}

func TestCircuitBreaker_Concurrency(t *testing.T) {
	cb := NewCircuitBreaker(Settings{
		Name: "test-cb",
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures > 1000
		},
	})

	const goroutines = 10
	const iterations = 100
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, _ = cb.Execute(func() (interface{}, error) {
					return "ok", nil
				})
			}
		}()
	}

	wg.Wait()

	expectedRequests := uint32(goroutines * iterations)
	if cb.Counts().Requests != expectedRequests {
		t.Errorf("Expected %d requests, got %d", expectedRequests, cb.Counts().Requests)
	}
}
