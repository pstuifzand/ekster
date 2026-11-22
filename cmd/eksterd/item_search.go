package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/lib/pq"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/userid"
)

// ItemSearchWithOptions selects authorized, filtered candidates from PostgreSQL
// in timeline order and checks bounded batches against Bleve for text matches.
func (b *memoryBackend) ItemSearchWithOptions(ctx context.Context, o microsub.ItemSearchOptions) (microsub.Timeline, error) {
	result := microsub.Timeline{Items: []microsub.Item{}}
	if err := microsub.ValidateItemSearchOptions(o); err != nil {
		return result, err
	}
	user, ok := userid.FromContext(ctx)
	if !ok {
		return result, fmt.Errorf("missing authenticated user")
	}
	for _, cursor := range []string{o.Before, o.After} {
		if cursor != "" {
			if _, _, _, err := searchCursor(cursor); err != nil {
				return result, fmt.Errorf("%w: %v", microsub.ErrInvalidItemSearch, err)
			}
		}
	}
	limit := o.Limit
	if limit == 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	args := []interface{}{user}
	var q strings.Builder
	q.WriteString(`SELECT i.id, i.uid, i.data, i.is_read, i.published_at FROM items i JOIN channels c ON c.id=i.channel_id`)
	if len(o.Source) > 0 {
		q.WriteString(` LEFT JOIN feeds f ON f.id=i.feed_id AND f.channel_id=i.channel_id`)
	}
	q.WriteString(` WHERE c.user_id=$1 AND i.published_at IS NOT NULL`)
	add := func(clause string, value interface{}) { args = append(args, value); fmt.Fprintf(&q, clause, len(args)) }
	if o.Channel != "global" {
		add(` AND c.uid=$%d`, o.Channel)
	}
	if o.Query != "" {
		if index == nil {
			return result, fmt.Errorf("search index unavailable")
		}
		if err := bleve.NewQueryStringQuery(o.Query).Validate(); err != nil {
			return result, fmt.Errorf("%w: invalid query: %v", microsub.ErrInvalidItemSearch, err)
		}
	}
	if len(o.Author) > 0 {
		add(` AND i.data->'author'->>'url'=ANY($%d)`, pq.Array(o.Author))
	}
	if len(o.Category) > 0 {
		add(` AND COALESCE(i.data->'category','[]'::jsonb) ?| $%d`, pq.Array(o.Category))
	}
	if len(o.Source) > 0 {
		add(` AND f.url=ANY($%d)`, pq.Array(o.Source))
	}
	if len(o.Kind) > 0 {
		var kinds []string
		for _, kind := range o.Kind {
			switch kind {
			case "like":
				kinds = append(kinds, `i.data ? 'like-of'`)
			case "repost":
				kinds = append(kinds, `i.data ? 'repost-of'`)
			case "bookmark":
				kinds = append(kinds, `i.data ? 'bookmark-of'`)
			case "reply":
				kinds = append(kinds, `i.data ? 'in-reply-to'`)
			case "checkin":
				kinds = append(kinds, `i.data ? 'checkin'`)
			case "photo", "video", "audio":
				kinds = append(kinds, fmt.Sprintf(`(jsonb_typeof(i.data->'%s')='array' AND i.data->'%s' <> '[]'::jsonb)`, kind, kind))
			}
		}
		if len(kinds) == 0 {
			return result, nil
		}
		q.WriteString(" AND (" + strings.Join(kinds, " OR ") + ")")
	}
	for _, bound := range []struct{ cursor, op string }{{o.Before, ">"}, {o.After, "<"}} {
		if bound.cursor == "" {
			continue
		}
		stamp, id, hasID, err := searchCursor(bound.cursor)
		if err != nil {
			return result, fmt.Errorf("%w: %v", microsub.ErrInvalidItemSearch, err)
		}
		args = append(args, stamp)
		n := len(args)
		if hasID {
			args = append(args, id)
			fmt.Fprintf(&q, ` AND (i.published_at %s $%d OR (i.published_at=$%d AND i.id %s $%d))`, bound.op, n, n, bound.op, len(args))
		} else {
			fmt.Fprintf(&q, ` AND i.published_at %s $%d`, bound.op, n)
		}
	}
	// Only inspect the authenticated user's already-filtered candidates. A
	// text search checks at most one bounded group of IDs against Bleve at a
	// time; sparse matches may require multiple cancellable database batches.
	const batchSize = 128
	type candidate struct {
		id        int
		uid       string
		item      microsub.Item
		read      int
		published time.Time
	}
	var first, last string
	var scanTime time.Time
	var scanID int
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		batchArgs := append([]interface{}(nil), args...)
		statement := q.String()
		if scanID != 0 {
			batchArgs = append(batchArgs, scanTime, scanID)
			statement += fmt.Sprintf(` AND (i.published_at, i.id) < ($%d, $%d)`, len(batchArgs)-1, len(batchArgs))
		}
		n := batchSize
		if o.Query == "" {
			n = limit + 1
		}
		batchArgs = append(batchArgs, n)
		statement += fmt.Sprintf(` ORDER BY i.published_at DESC, i.id DESC LIMIT $%d`, len(batchArgs))
		rows, err := b.database.QueryContext(ctx, statement, batchArgs...)
		if err != nil {
			return result, err
		}
		candidates := make([]candidate, 0, n)
		for rows.Next() {
			var row candidate
			if err := rows.Scan(&row.id, &row.uid, &row.item, &row.read, &row.published); err != nil {
				rows.Close()
				return result, err
			}
			candidates = append(candidates, row)
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return result, err
		}
		if closeErr != nil {
			return result, closeErr
		}
		if len(candidates) == 0 {
			break
		}
		var matching map[string]bool
		if o.Query != "" {
			ids := make([]string, 0, len(candidates))
			for _, row := range candidates {
				ids = append(ids, row.uid)
			}
			matching, err = searchCandidateIDs(ctx, o.Query, ids)
			if err != nil {
				return result, err
			}
		}
		for _, row := range candidates {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			if o.Query != "" && !matching[row.uid] {
				continue
			}
			if len(result.Items) == limit {
				result.Paging.After = last
				result.Paging.Before = first
				return result, nil
			}
			row.item.ID = row.uid
			row.item.Read = row.read == 1
			row.item.Published = row.published.UTC().Format(time.RFC3339Nano)
			cursor := fmt.Sprintf("%s|%d", row.item.Published, row.id)
			if first == "" {
				first = cursor
			}
			last = cursor
			result.Items = append(result.Items, row.item)
		}
		if len(candidates) < n {
			break
		}
		tail := candidates[len(candidates)-1]
		scanTime, scanID = tail.published, tail.id
	}
	result.Paging.Before = first
	return result, nil
}

func searchCandidateIDs(ctx context.Context, text string, ids []string) (map[string]bool, error) {
	matching := make(map[string]bool)
	if len(ids) == 0 {
		return matching, nil
	}
	query := bleve.NewConjunctionQuery(bleve.NewDocIDQuery(ids), bleve.NewQueryStringQuery(text))
	request := bleve.NewSearchRequestOptions(query, len(ids), 0, false)
	hits, err := index.SearchInContext(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("search index: %w", err)
	}
	for _, hit := range hits.Hits {
		matching[hit.ID] = true
	}
	return matching, nil
}

func searchCursor(cursor string) (time.Time, int, bool, error) {
	stamp := cursor
	id := 0
	hasID := false
	if pos := strings.LastIndexByte(cursor, '|'); pos >= 0 {
		stamp = cursor[:pos]
		n, err := strconv.Atoi(cursor[pos+1:])
		if err != nil || n <= 0 {
			return time.Time{}, 0, false, fmt.Errorf("invalid cursor")
		}
		id = n
		hasID = true
	}
	t, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return time.Time{}, 0, false, fmt.Errorf("invalid cursor")
	}
	return t, id, hasID, nil
}
