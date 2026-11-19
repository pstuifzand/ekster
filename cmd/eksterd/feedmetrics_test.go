package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// A feed that has been rescheduled but not fetched since must still report its
// tier: the whole point of reading the table is that no fetch span is involved.
func TestFeedGaugesReportTierAndNextFetch(t *testing.T) {
	next := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	gauges := newFeedGauges(nil)
	gauges.list = func() ([]feedSchedule, error) {
		return []feedSchedule{{
			URL: "https://a.example/feed.xml", Tier: 7, NextFetchAt: next,
			LastHTTPStatus: 401, Failures: 3,
		}}, nil
	}

	want := `
# HELP ekster_feed_tier Tier a feed is on; the fetch interval is 2^tier minutes.
# TYPE ekster_feed_tier gauge
ekster_feed_tier{url="https://a.example/feed.xml"} 7
# HELP ekster_feed_last_http_status HTTP status of the last fetch; 0 when the request produced no response.
# TYPE ekster_feed_last_http_status gauge
ekster_feed_last_http_status{url="https://a.example/feed.xml"} 401
# HELP ekster_feed_consecutive_failures Fetches in a row that did not succeed.
# TYPE ekster_feed_consecutive_failures gauge
ekster_feed_consecutive_failures{url="https://a.example/feed.xml"} 3
# HELP ekster_feed_next_fetch_timestamp_seconds Unix time at which the scheduler will next fetch the feed.
# TYPE ekster_feed_next_fetch_timestamp_seconds gauge
ekster_feed_next_fetch_timestamp_seconds{url="https://a.example/feed.xml"} 1.7948304e+09
`
	if err := testutil.CollectAndCompare(gauges, strings.NewReader(want)); err != nil {
		t.Fatal(err)
	}
}

// A failed query emits no series at all. A zero would read as "tier 0, due in
// 1970", which is a fact the dashboard would then show.
func TestFeedGaugesEmitNothingWithoutTheDatabase(t *testing.T) {
	gauges := newFeedGauges(nil)
	gauges.list = func() ([]feedSchedule, error) { return nil, errors.New("database is gone") }

	registry := prometheus.NewRegistry()
	if err := registry.Register(gauges); err != nil {
		t.Fatalf("register: %v", err)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	if len(families) != 0 {
		t.Fatalf("gathered %d metric families after a failed query, want 0", len(families))
	}
}

// A feed with no next_fetch_at is due on the next tick: it has no timestamp to
// report, and reporting one would be invented.
func TestFeedGaugesSkipAFeedWithNoDueTime(t *testing.T) {
	gauges := newFeedGauges(nil)
	gauges.list = func() ([]feedSchedule, error) {
		return []feedSchedule{{URL: "https://a.example/feed.xml", Tier: 3}}, nil
	}

	registry := prometheus.NewRegistry()
	if err := registry.Register(gauges); err != nil {
		t.Fatalf("register: %v", err)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, family := range families {
		if family.GetName() == "ekster_feed_next_fetch_timestamp_seconds" && len(family.GetMetric()) != 0 {
			t.Fatalf("a feed with no due time produced %d series", len(family.GetMetric()))
		}
	}
}
