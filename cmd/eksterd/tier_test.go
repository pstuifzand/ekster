package main

import (
	"testing"
	"time"
)

// A feed that changed must land two rungs lower and, crucially, have its climb
// reset: otherwise a change one fetch into a climb is handed back by the very
// next quiet fetch and the drop never took effect.
func TestChangedFeedDropsTwoTiersAndResetsTheClimb(t *testing.T) {
	b := &memoryBackend{}
	f := feed{Tier: 10, Unmodified: 1}

	b.scheduleNextFetch(&f, true)

	if f.Tier != 8 {
		t.Fatalf("tier = %d, want 8 (two rungs below 10)", f.Tier)
	}
	if f.Unmodified != 0 {
		t.Fatalf("unmodified = %d, want 0 after a change", f.Unmodified)
	}
	if got, want := f.NextFetchAt.Sub(time.Now()), 256*time.Minute; got > want+time.Minute || got < want-time.Minute {
		t.Fatalf("next fetch in %v, want about 2^8 minutes", got)
	}
}

// Two quiet fetches buy one rung, not one per fetch.
func TestUnchangedFeedClimbsOneTierPerTwoFetches(t *testing.T) {
	b := &memoryBackend{}
	f := feed{Tier: 10}

	b.scheduleNextFetch(&f, false)
	if f.Tier != 10 || f.Unmodified != 1 {
		t.Fatalf("after one quiet fetch: tier %d, unmodified %d", f.Tier, f.Unmodified)
	}

	b.scheduleNextFetch(&f, false)
	if f.Tier != 11 || f.Unmodified != 0 {
		t.Fatalf("after two quiet fetches: tier %d, unmodified %d", f.Tier, f.Unmodified)
	}
}

// The ceiling is a real ceiling: a feed cannot climb past it, and the interval
// it implies is the reason the number is not larger.
func TestTierIsClampedAtTheCeiling(t *testing.T) {
	b := &memoryBackend{}
	f := feed{Tier: maxTier, Unmodified: 1}

	b.scheduleNextFetch(&f, false)
	if f.Tier != maxTier {
		t.Fatalf("tier = %d, want the ceiling %d", f.Tier, maxTier)
	}

	// 2^20 minutes is about two years; if this ever reads as hours, the
	// exponent changed meaning.
	if interval := time.Duration(1<<maxTier) * time.Minute; interval < 700*24*time.Hour {
		t.Fatalf("ceiling interval = %v, want a couple of years", interval)
	}
}

// A feed at the bottom cannot fall below zero, however often it changes.
func TestTierFloorsAtZero(t *testing.T) {
	b := &memoryBackend{}
	f := feed{Tier: 1}

	b.scheduleNextFetch(&f, true)

	if f.Tier != 0 {
		t.Fatalf("tier = %d, want 0", f.Tier)
	}
}

// A new subscription starts above the eager end of the ladder: the interval
// after its immediate first fetch is an hour, not two minutes.
func TestNewFeedsStartAtTheStartTier(t *testing.T) {
	if startTier != 6 {
		t.Fatalf("startTier = %d, want 6", startTier)
	}
	if interval := time.Duration(1<<startTier) * time.Minute; interval != 64*time.Minute {
		t.Fatalf("start interval = %v, want 64m", interval)
	}
}
