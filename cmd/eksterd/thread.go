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
	"database/sql"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/lib/pq"
	"github.com/pstuifzand/ekster/pkg/fetch"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/observability"
	"github.com/pstuifzand/ekster/pkg/userid"

	"go.opentelemetry.io/otel/attribute"
)

const (
	// A thread is capped, not paged. These bounds are the only thing between
	// one request and an unbounded amount of outbound work.
	threadFetchTimeout = 5 * time.Second
	threadBudget       = 12 * time.Second
	maxThreadItems     = 200
	maxReplyLevels     = 3
)

// ThreadGet returns the conversation around one entry.
//
// The set of posts a server can see is its subscriptions; the set of posts in
// a conversation is not. So the server does both halves differently: replies
// come only from entries it has already indexed, while ancestors are walked up
// the in-reply-to chain and fetched when they are missing. Only ancestors are
// fetched, and only through publicFetcher, because a feed can point an entry
// at any URL it likes.
func (b *memoryBackend) ThreadGet(ctx context.Context, channel, entry, targetURL string, depth int) (microsub.Thread, error) {
	ctx, span := observability.Span(ctx, "db.thread.get",
		attribute.String("channel", channel), attribute.Int("depth", depth))
	defer span.End()

	userID, _ := userid.FromContext(ctx)

	if depth <= 0 {
		depth = microsub.DefaultThreadDepth
	}
	if depth > microsub.MaxThreadDepth {
		depth = microsub.MaxThreadDepth
	}

	ctx, cancel := context.WithTimeout(ctx, threadBudget)
	defer cancel()

	anchor, err := b.threadAnchor(ctx, userID, channel, entry, targetURL)
	if err != nil {
		return microsub.Thread{Items: []microsub.Item{}}, err
	}

	fetcher := b.threadFetcher()

	// Walk up. The chain is kept root-first, so the item we are extending is
	// always chain[0]; prepending is cheap enough at depth 20.
	seen := map[string]bool{threadRef(anchor): true}
	chain := []microsub.Item{anchor}
	missing := 0
	partial := false

	for hops := 0; hops < depth; hops++ {
		parentURL := firstReplyTarget(chain[0])
		if parentURL == "" || seen[parentURL] {
			break
		}
		seen[parentURL] = true

		parent, ok, err := b.threadItem(ctx, userID, "", parentURL)
		if err != nil {
			return microsub.Thread{}, err
		}
		if ok {
			chain = append([]microsub.Item{parent}, chain...)
			continue
		}

		if ctx.Err() != nil {
			missing++
			partial = true
			break
		}

		fetchCtx, fetchCancel := context.WithTimeout(ctx, threadFetchTimeout)
		parent, fetchErr := itemAtURL(fetchCtx, fetcher, parentURL)
		fetchCancel()
		if fetchErr != nil {
			// The ancestor is known to exist and could not be read: that is
			// exactly what partial and missing are for. Stop walking, because
			// without the parent the rest of the chain is unreachable.
			slog.DebugContext(ctx, "thread ancestor could not be resolved",
				"url", parentURL, "error", fetchErr)
			missing++
			partial = true
			break
		}
		// Fetched, so outside the user's channels: no _id, no read state.
		parent.ID = ""
		parent.Read = false
		chain = append([]microsub.Item{parent}, chain...)
	}

	// Ran out of depth with the chain still going up. The client is told the
	// thread is partial rather than shown an arbitrary cut as the root.
	if top := firstReplyTarget(chain[0]); top != "" && !seen[top] {
		partial = true
	}

	frontier := []string{}
	for _, item := range chain {
		for _, ref := range []string{item.URL, item.UID, item.ID} {
			if ref != "" && !containsString(frontier, ref) {
				frontier = append(frontier, ref)
			}
		}
	}

	known := map[string]bool{}
	for _, item := range chain {
		if item.ID != "" {
			known[item.ID] = true
		}
		if item.UID != "" {
			known[item.UID] = true
		}
	}

	replies, err := b.threadReplies(ctx, userID, frontier, known)
	if err != nil {
		return microsub.Thread{}, err
	}

	items := make([]microsub.Item, 0, len(chain)+len(replies))
	items = append(items, chain...)
	items = append(items, replies...)

	root := ""
	if len(chain) > 0 {
		root = chain[0].URL
		if root == "" {
			root = chain[0].UID
		}
	}

	return microsub.Thread{
		Items: items,
		Thread: microsub.ThreadInfo{
			Root:    root,
			Partial: partial,
			Missing: missing,
		},
	}, nil
}

// threadAnchor resolves the entry the client asked about. An unknown _id is
// not found; an unknown URL may still be a real page, so it is fetched once.
func (b *memoryBackend) threadAnchor(ctx context.Context, userID int, channel, entry, targetURL string) (microsub.Item, error) {
	if entry != "" {
		item, ok, err := b.threadItem(ctx, userID, channel, entry)
		if err != nil {
			return microsub.Item{}, err
		}
		if !ok {
			return microsub.Item{}, fmt.Errorf("%w: entry %q", microsub.ErrNotFound, entry)
		}
		return item, nil
	}

	item, ok, err := b.threadItem(ctx, userID, channel, targetURL)
	if err != nil {
		return microsub.Item{}, err
	}
	if ok {
		return item, nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, threadFetchTimeout)
	defer cancel()
	fetched, err := itemAtURL(fetchCtx, b.threadFetcher(), targetURL)
	if err != nil {
		return microsub.Item{}, fmt.Errorf("%w: %s", microsub.ErrNotFound, targetURL)
	}
	fetched.ID = ""
	fetched.Read = false
	return fetched, nil
}

// threadFetcher is the fetcher for URLs the server chose for itself. It caches
// the document when there is a cache to use: the same ancestor is asked for by
// every thread that reaches it, and a thread must not re-fetch a page per
// client.
func (b *memoryBackend) threadFetcher() fetch.Fetcher {
	fetcher := publicFetcher(threadFetchTimeout)
	if b.pool == nil {
		// The integration tests build the backend by hand; a missing cache
		// must not be a nil dereference.
		return fetcher
	}
	return WithCaching(b.pool, fetcher)
}

// threadItem finds an indexed entry by _id or by permalink, scoped to the
// user's channels. Another user's entry is indistinguishable from one that
// does not exist.
func (b *memoryBackend) threadItem(ctx context.Context, userID int, channel, ref string) (microsub.Item, bool, error) {
	var item microsub.Item
	var uid string
	var isRead int
	var published sql.NullTime

	err := b.database.QueryRowContext(ctx, `
SELECT i."uid", i."data", i."is_read", i."published_at"
FROM "items" AS "i"
INNER JOIN "channels" AS "c" ON c."id" = i."channel_id"
WHERE c."user_id" = $1
  AND ($2 = '' OR c."uid" = $2)
  AND (i."uid" = $3 OR i."data"->>'url' = $3)
ORDER BY i."id"
LIMIT 1
`, userID, channel, ref).Scan(&uid, &item, &isRead, &published)
	if err == sql.ErrNoRows {
		return microsub.Item{}, false, nil
	}
	if err != nil {
		return microsub.Item{}, false, fmt.Errorf("thread item %q: %w", ref, err)
	}

	item.ID = uid
	item.Read = isRead == 1
	if published.Valid {
		item.Published = published.Time.UTC().Format(time.RFC3339)
	}
	return item, true, nil
}

// threadReplies finds the indexed entries that answer anything in the thread.
// Replies are never fetched: the set of them is the user's subscriptions, and
// crawling the web for replies would turn one request into an unbounded crawl.
func (b *memoryBackend) threadReplies(ctx context.Context, userID int, frontier []string, known map[string]bool) ([]microsub.Item, error) {
	replies := []microsub.Item{}
	if len(frontier) == 0 {
		return replies, nil
	}

	seen := map[string]bool{}
	for _, ref := range frontier {
		seen[ref] = true
	}
	next := frontier

	for level := 0; level < maxReplyLevels && len(next) > 0 && len(replies) < maxThreadItems; level++ {
		rows, err := b.database.QueryContext(ctx, `
SELECT i."uid", i."data", i."is_read", i."published_at"
FROM "items" AS "i"
INNER JOIN "channels" AS "c" ON c."id" = i."channel_id"
WHERE c."user_id" = $1
  AND i."data"->'in-reply-to' ?| $2::text[]
ORDER BY i."published_at" ASC NULLS LAST, i."id" ASC
`, userID, pq.Array(next))
		if err != nil {
			return nil, fmt.Errorf("thread replies: %w", err)
		}

		children := []string{}
		for rows.Next() {
			var item microsub.Item
			var uid string
			var isRead int
			var published sql.NullTime
			if err := rows.Scan(&uid, &item, &isRead, &published); err != nil {
				rows.Close()
				return nil, fmt.Errorf("thread replies: %w", err)
			}
			if known[uid] {
				continue
			}
			known[uid] = true
			item.ID = uid
			item.Read = isRead == 1
			if published.Valid {
				item.Published = published.Time.UTC().Format(time.RFC3339)
			}
			replies = append(replies, item)

			for _, ref := range []string{item.URL, uid} {
				if ref != "" && !seen[ref] {
					seen[ref] = true
					children = append(children, ref)
				}
			}
			if len(replies) >= maxThreadItems {
				break
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("thread replies: %w", err)
		}
		rows.Close()
		next = children
	}

	// Conversation order, oldest first. An entry without a usable published
	// time sorts after the ones that have one, so undated posts do not push
	// themselves to the top of a conversation.
	sort.SliceStable(replies, func(i, j int) bool {
		a, aok := parseThreadTime(replies[i].Published)
		c, cok := parseThreadTime(replies[j].Published)
		if aok != cok {
			return aok
		}
		if !aok {
			return false
		}
		return a.Before(c)
	})

	return replies, nil
}

// firstReplyTarget is the parent an entry answers. Multiple in-reply-to values
// are legal; the conversation reads up one chain, so the first is the one.
func firstReplyTarget(item microsub.Item) string {
	for _, target := range item.InReplyTo {
		if target != "" {
			return target
		}
	}
	return ""
}

// threadRef is how one entry is named when building the reply frontier.
func threadRef(item microsub.Item) string {
	if item.URL != "" {
		return item.URL
	}
	if item.UID != "" {
		return item.UID
	}
	return item.ID
}

func parseThreadTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
