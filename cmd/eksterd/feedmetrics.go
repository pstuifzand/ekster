package main

import (
	"database/sql"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// feedSchedule is the scheduler's own state for one feed: the tier it is on and
// the moment it is due. It is read from the feeds table, which is what the
// ticker itself queries, so a gauge built from it answers "when will this be
// fetched" with the same answer the scheduler would give.
type feedSchedule struct {
	URL         string
	Tier        int
	NextFetchAt time.Time
}

// feedGauges exports that state per feed. It exists because tier and next fetch
// are current state, not something a fetch span can carry: a feed on tier 10 is
// fetched once every seventeen hours, so the newest span is already stale, and a
// feed that was just rescheduled has not been fetched at all since the change.
//
// The list function is injected so the collector can be exercised without a
// database: Collect must emit what the query returned and nothing when it fails.
type feedGauges struct {
	list func() ([]feedSchedule, error)

	tier *prometheus.Desc
	next *prometheus.Desc
}

// newFeedGauges wires the collector to the live feeds table.
func newFeedGauges(db *sql.DB) *feedGauges {
	return &feedGauges{
		list: func() ([]feedSchedule, error) {
			rows, err := db.Query(`SELECT "url", "tier", "next_fetch_at" FROM "feeds"`)
			if err != nil {
				return nil, err
			}
			defer rows.Close()

			var feeds []feedSchedule
			for rows.Next() {
				var (
					f    feedSchedule
					next sql.NullTime
				)
				if err := rows.Scan(&f.URL, &f.Tier, &next); err != nil {
					return nil, err
				}
				if next.Valid {
					f.NextFetchAt = next.Time
				}
				feeds = append(feeds, f)
			}
			return feeds, rows.Err()
		},
		tier: prometheus.NewDesc(
			"ekster_feed_tier",
			"Tier a feed is on; the fetch interval is 2^tier minutes.",
			[]string{"url"}, nil),
		next: prometheus.NewDesc(
			"ekster_feed_next_fetch_timestamp_seconds",
			"Unix time at which the scheduler will next fetch the feed.",
			[]string{"url"}, nil),
	}
}

func (g *feedGauges) Describe(ch chan<- *prometheus.Desc) {
	ch <- g.tier
	ch <- g.next
}

// Collect runs the query per scrape: 49 rows every 30 seconds is cheaper than a
// second copy of the schedule that can drift from the one the ticker reads.
func (g *feedGauges) Collect(ch chan<- prometheus.Metric) {
	feeds, err := g.list()
	if err != nil {
		// Emit nothing rather than a zero: a tier of 0 and a fetch due in 1970
		// would both look like facts on the dashboard.
		slog.Error("metrics: could not read the per-feed schedule", "error", err)
		return
	}
	for _, f := range feeds {
		ch <- prometheus.MustNewConstMetric(g.tier, prometheus.GaugeValue, float64(f.Tier), f.URL)
		if !f.NextFetchAt.IsZero() {
			ch <- prometheus.MustNewConstMetric(g.next, prometheus.GaugeValue, float64(f.NextFetchAt.Unix()), f.URL)
		}
	}
}
