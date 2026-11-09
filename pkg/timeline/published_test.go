package timeline

import (
	"testing"
	"time"
)

func TestParsePublished(t *testing.T) {
	// What microformats h-entries actually carry. The second form is the one
	// that rejected every item of ramblinggit.com and svenknebel.de for years.
	accepted := []string{
		"2026-11-06T16:38:41Z",
		"2026-11-06T16:38:41+01:00",
		"2026-11-06T16:38:41+0100",
		"2026-11-06 16:38:41 -0600",
		"2026-11-06 16:38:41",
		"2026-11-06",
		" 2026-11-06T16:38:41Z ",
		"Mon, 06 Nov 2026 16:38:41 -0600",
	}

	for _, value := range accepted {
		if _, err := parsePublished(value); err != nil {
			t.Errorf("parsePublished(%q) failed: %s", value, err)
		}
	}

	want := time.Date(2026, 11, 6, 22, 38, 41, 0, time.UTC)
	got, err := parsePublished("2026-11-06 16:38:41 -0600")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if !got.Equal(want) {
		t.Errorf("parsePublished() = %s, want %s", got, want)
	}

	for _, value := range []string{"", "yesterday", "06/11/2026"} {
		if _, err := parsePublished(value); err == nil {
			t.Errorf("parsePublished(%q) should fail", value)
		}
	}
}
