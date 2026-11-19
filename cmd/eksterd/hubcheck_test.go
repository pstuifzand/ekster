/*
 *  Ekster is a microsub server
 *  Copyright (c) 2021 The Ekster authors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Discovery is retried on a ladder, not on every pass. The rungs are the whole
// policy, so they are asserted as numbers: a subscription is asked again after
// an hour, a day, and then monthly, and a feed that never declares a hub costs
// twelve requests a year instead of one every minute.
func TestHubCheckLadder(t *testing.T) {
	assert.Equal(t, 4, len(hubCheckLadder), "the ladder is meant to be short")
	assert.Equal(t, time.Duration(0), hubCheckDelay(0))
	assert.Equal(t, time.Hour, hubCheckDelay(1))
	assert.Equal(t, 24*time.Hour, hubCheckDelay(2))
	assert.Equal(t, 30*24*time.Hour, hubCheckDelay(3))

	// The last rung repeats rather than running out: a subscription is never
	// left un-checked forever, and the count of attempts stops growing cost.
	assert.Equal(t, 30*24*time.Hour, hubCheckDelay(4))
	assert.Equal(t, 30*24*time.Hour, hubCheckDelay(100))

	// A negative count cannot happen, but it must not index the slice.
	assert.Equal(t, time.Duration(0), hubCheckDelay(-2))
}

// A row that has never been checked is due: subscriptions created before the
// ladder existed have no hub_checked_at, and they have to be asked once.
func TestHubCheckDue(t *testing.T) {
	now := time.Date(2026, 11, 19, 22, 0, 0, 0, time.UTC)

	assert.True(t, hubCheckDue(nil, 0, now), "a never-checked subscription is due")
	assert.True(t, hubCheckDue(nil, 7, now), "missing timestamp wins over the count")

	oneHourAgo := now.Add(-61 * time.Minute)
	assert.True(t, hubCheckDue(&oneHourAgo, 1, now), "an hour has passed")

	recent := now.Add(-30 * time.Minute)
	assert.False(t, hubCheckDue(&recent, 1, now), "thirty minutes has not")

	// After the ladder runs out, thirty days is the period.
	monthAgo := now.Add(-30*24*time.Hour + time.Minute)
	assert.False(t, hubCheckDue(&monthAgo, 3, now), "just short of a month waits")
	pastMonth := now.Add(-30 * 24 * time.Hour)
	assert.True(t, hubCheckDue(&pastMonth, 9, now), "a month has passed")
}

// The longest wait a dormant feed can be given is 30 days, whatever the top
// rung computes to: 2^16 minutes is about 45, and a reader who comes back to a
// month-old feed should find it was read in the last month.
func TestScheduleNextFetchCapsTheIntervalAtThirtyDays(t *testing.T) {
	b := &memoryBackend{}

	f := feed{ID: 3, URL: "https://p83.nl/", Tier: maxTier, Unmodified: climbAfter - 1}
	b.scheduleNextFetch(context.Background(), &f, false)

	wait := time.Until(f.NextFetchAt)
	assert.InDelta(t, float64(maxInterval), float64(wait), float64(10*time.Second))
	assert.Less(t, float64(wait), float64(31*24*time.Hour))
}
