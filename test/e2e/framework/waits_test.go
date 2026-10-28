package framework

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWaitForConditionSucceedsWhenConditionFlips(t *testing.T) {
	calls := 0
	err := WaitForCondition(context.Background(),
		WaitOptions{Timeout: 5 * time.Second, Interval: 10 * time.Millisecond, Description: "flip"},
		func(context.Context) (bool, error) {
			calls++
			return calls >= 3, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if calls < 3 {
		t.Fatalf("condition polled %d times, want >= 3", calls)
	}
}

func TestWaitForConditionTimeoutErrorIsActionable(t *testing.T) {
	start := time.Now()
	err := WaitForCondition(context.Background(),
		WaitOptions{Timeout: 100 * time.Millisecond, Interval: 10 * time.Millisecond, Description: "never-ready thing"},
		func(context.Context) (bool, error) { return false, nil })
	if err == nil {
		t.Fatal("expected timeout error")
	}
	for _, want := range []string{"100ms", "never-ready thing", "condition not yet true"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("timeout error %q missing %q", err.Error(), want)
		}
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("wait outlived its deadline: %s", elapsed)
	}
}

func TestWaitForConditionReportsLastObservationError(t *testing.T) {
	err := WaitForCondition(context.Background(),
		WaitOptions{Timeout: 100 * time.Millisecond, Interval: 10 * time.Millisecond, Description: "flaky get"},
		func(context.Context) (bool, error) { return false, errors.New("connection refused") })
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("expected last observation error in timeout, got %v", err)
	}
}

func TestWaitForConditionPropagatesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := WaitForCondition(ctx,
		WaitOptions{Timeout: time.Minute, Interval: 10 * time.Millisecond, Description: "cancelled"},
		func(context.Context) (bool, error) { return false, nil })
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected cancellation-aware error, got %v", err)
	}
}
