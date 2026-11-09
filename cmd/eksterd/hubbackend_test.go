package main

import (
	"testing"
	"time"
)

func TestSubscribeBackoff(t *testing.T) {
	if got := subscribeBackoff(1); got != 15*time.Minute {
		t.Errorf("first retry = %s, want 15m", got)
	}
	if got := subscribeBackoff(2); got != 30*time.Minute {
		t.Errorf("second retry = %s, want 30m", got)
	}
	previous := time.Duration(0)
	for failures := 1; failures <= 12; failures++ {
		got := subscribeBackoff(failures)
		if got > 6*time.Hour {
			t.Fatalf("failures=%d gives %s, above the 6h cap", failures, got)
		}
		if got < previous {
			t.Fatalf("failures=%d gives %s, less than the previous %s", failures, got, previous)
		}
		previous = got
	}
	if got := subscribeBackoff(12); got != 6*time.Hour {
		t.Errorf("backoff does not reach the 6h cap: %s", got)
	}
}
