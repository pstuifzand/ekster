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

package timeline

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/pstuifzand/ekster/pkg/microsub"

	// load pq for postgres
	"github.com/lib/pq"
)

type postgresStream struct {
	database  *sql.DB
	channel   string
	channelID int
}

// Init
func (p *postgresStream) Init() error {
	ctx := context.Background()
	conn, err := p.database.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	err = conn.PingContext(ctx)
	if err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	row := conn.QueryRowContext(ctx, `SELECT "id" FROM "channels" WHERE "uid" = $1`, p.channel)
	err = row.Scan(&p.channelID)
	// if err == sql.ErrNoRows {
	// 	return fmt.Errorf("channel %s not found: %w", p.channel, err)
	// }

	return nil
}

// Items returns a page ordered by publication time, with the database ID as a
// stable tie-breaker. Cursors include both values so equal publication times
// cannot cause entries to be skipped between pages. Timestamp-only cursors are
// still accepted for compatibility with clients holding older cursors.
func (p *postgresStream) Items(before, after string) (microsub.Timeline, error) {
	conn, err := p.database.Conn(context.Background())
	if err != nil {
		return microsub.Timeline{}, err
	}
	defer conn.Close()

	var args []interface{}
	args = append(args, p.channelID)
	var qb strings.Builder
	qb.WriteString(`
SELECT "id", "uid", "data", "created_at", "is_read", "published_at"
FROM "items"
WHERE "channel_id" = $1
`)
	cursor := before
	if cursor == "" {
		cursor = after
	}
	if cursor != "" {
		publishedAt, id, hasID, err := parseItemCursor(cursor)
		if err != nil {
			return microsub.Timeline{}, err
		}
		args = append(args, publishedAt)
		if before != "" {
			if hasID {
				args = append(args, id)
				qb.WriteString(` AND ("published_at" > $2 OR ("published_at" = $2 AND "id" > $3))`)
			} else {
				qb.WriteString(` AND "published_at" > $2`)
			}
		} else if hasID {
			args = append(args, id)
			qb.WriteString(` AND ("published_at" < $2 OR ("published_at" = $2 AND "id" < $3))`)
		} else {
			qb.WriteString(` AND "published_at" < $2`)
		}
	}
	qb.WriteString(` ORDER BY "published_at" DESC, "id" DESC LIMIT 20`)

	rows, err := conn.QueryContext(context.Background(), qb.String(), args...)
	if err != nil {
		return microsub.Timeline{}, fmt.Errorf("while query: %w", err)
	}

	var tl microsub.Timeline
	var firstCursor, lastCursor string
	for rows.Next() {
		var id int
		var uid string
		var item microsub.Item
		var createdAt time.Time
		var isRead int
		var publishedAt time.Time
		if err := rows.Scan(&id, &uid, &item, &createdAt, &isRead, &publishedAt); err != nil {
			_ = rows.Close()
			return tl, err
		}
		item.Read = isRead == 1
		item.ID = uid
		// Postgres prints a timestamptz as "2006-01-02 15:04:05+00", which is neither
		// what clients parse nor what parseItemCursor accepts: canonical RFC3339 goes
		// into both the item and the cursor, which is what makes the cursor round trip.
		stamp := publishedAt.UTC().Format(time.RFC3339)
		item.Published = stamp
		tl.Items = append(tl.Items, item)
		itemCursor := makeItemCursor(stamp, id)
		if firstCursor == "" {
			firstCursor = itemCursor
		}
		lastCursor = itemCursor
	}
	if err := rows.Close(); err != nil {
		return tl, err
	}
	if err := rows.Err(); err != nil {
		return tl, err
	}

	if firstCursor != "" && p.hasMoreBefore(conn, firstCursor) {
		tl.Paging.Before = firstCursor
	}
	if lastCursor != "" && p.hasMoreAfter(conn, lastCursor) {
		tl.Paging.After = lastCursor
	}
	if tl.Items == nil {
		tl.Items = []microsub.Item{}
	}
	return tl, nil
}

func makeItemCursor(publishedAt string, id int) string {
	return fmt.Sprintf("%s|%d", publishedAt, id)
}

func parseItemCursor(cursor string) (time.Time, int, bool, error) {
	publishedAt := cursor
	var id int
	hasID := false
	if separator := strings.LastIndexByte(cursor, '|'); separator >= 0 {
		publishedAt = cursor[:separator]
		parsedID, err := strconv.Atoi(cursor[separator+1:])
		if err != nil {
			return time.Time{}, 0, false, fmt.Errorf("invalid timeline cursor %q: %w", cursor, err)
		}
		id, hasID = parsedID, true
	}
	parsedTime, err := parseCursorTime(publishedAt)
	if err != nil {
		return time.Time{}, 0, false, fmt.Errorf("invalid timeline cursor %q: %w", cursor, err)
	}
	return parsedTime, id, hasID, nil
}

// cursorTimeLayouts is RFC3339 first, then the shapes a cursor can arrive in from an
// older client or an older server that passed the Postgres text through verbatim.
var cursorTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999-07",
	"2006-01-02 15:04:05-07",
	"2006-01-02 15:04:05-07:00",
	"2006-01-02 15:04:05",
}

func parseCursorTime(value string) (time.Time, error) {
	var lastErr error
	for _, layout := range cursorTimeLayouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

func (p *postgresStream) hasMoreBefore(conn *sql.Conn, cursor string) bool {
	publishedAt, id, hasID, err := parseItemCursor(cursor)
	if err != nil {
		return false
	}
	query := `SELECT EXISTS (SELECT 1 FROM "items" WHERE "channel_id" = $1 AND "published_at" > $2)`
	args := []interface{}{p.channelID, publishedAt}
	if hasID {
		query = `SELECT EXISTS (SELECT 1 FROM "items" WHERE "channel_id" = $1 AND ("published_at" > $2 OR ("published_at" = $2 AND "id" > $3)))`
		args = append(args, id)
	}
	var exists bool
	if err := conn.QueryRowContext(context.Background(), query, args...).Scan(&exists); err != nil {
		return false
	}
	return exists
}

func (p *postgresStream) hasMoreAfter(conn *sql.Conn, cursor string) bool {
	publishedAt, id, hasID, err := parseItemCursor(cursor)
	if err != nil {
		return false
	}
	query := `SELECT EXISTS (SELECT 1 FROM "items" WHERE "channel_id" = $1 AND "published_at" < $2)`
	args := []interface{}{p.channelID, publishedAt}
	if hasID {
		query = `SELECT EXISTS (SELECT 1 FROM "items" WHERE "channel_id" = $1 AND ("published_at" < $2 OR ("published_at" = $2 AND "id" < $3)))`
		args = append(args, id)
	}
	var exists bool
	if err := conn.QueryRowContext(context.Background(), query, args...).Scan(&exists); err != nil {
		return false
	}
	return exists
}

// Count
func (p *postgresStream) Count() (int, error) {
	ctx := context.Background()
	conn, err := p.database.Conn(ctx)
	if err != nil {
		return -1, err
	}
	defer conn.Close()
	var count int
	row := conn.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM items WHERE channel_id = $1 AND "is_read" = 0`, p.channelID)
	err = row.Scan(&count)
	if err != nil && err == sql.ErrNoRows {
		return 0, nil
	}
	return count, nil
}

// publishedLayouts covers what microformats pages actually emit. The value is
// copied from the markup verbatim, so besides RFC3339 the plain
// "2006-01-02 15:04:05 -0700" form most themes print has to be accepted;
// otherwise every item of an otherwise valid h-feed is rejected at insert time
// and the feed looks empty for years.
var publishedLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05Z0700",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02",
	time.RFC1123Z,
	time.RFC822Z,
}

func parsePublished(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range publishedLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("time %q could not be parsed with any known layout", value)
}

// AddItem
func (p *postgresStream) AddItem(item microsub.Item) (bool, error) {
	ctx := context.Background()
	conn, err := p.database.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	t, err := parsePublished(item.Published)
	if err != nil {
		return false, fmt.Errorf("while adding item: %w", err)
	}
	if item.ID == "" {
		// FIXME: This won't work when we receive the item multiple times
		h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", p.channel, time.Now().UnixNano())))
		item.UID = hex.EncodeToString(h[:])
	}

	var optFeedID sql.NullInt64
	if item.Source == nil || item.Source.ID == "" {
		optFeedID.Valid = false
		optFeedID.Int64 = 0
	} else {
		feedID, err := strconv.ParseInt(item.Source.ID, 10, 64)
		if err != nil {
			optFeedID.Valid = false
			optFeedID.Int64 = 0
		} else {
			optFeedID.Valid = true
			optFeedID.Int64 = feedID
		}
	}

	// An item that comes back changed - an edited entry, or the same entry now
	// carrying its author - replaces what is stored. Leaving the old copy in
	// place means a feed can never correct itself. is_read lives in its own
	// column, so rewriting data does not mark anything unread.
	//
	// xmax is zero only for a fresh insert, which is what the caller reports as
	// a new item; an update is a correction, not news.
	var inserted bool
	err = conn.QueryRowContext(context.Background(), `
INSERT INTO "items" ("channel_id", "feed_id", "uid", "data", "published_at", "created_at")
VALUES ($1, $2, $3, $4, $5, DEFAULT)
ON CONFLICT ON CONSTRAINT "items_uid_key" DO UPDATE
SET "data" = EXCLUDED."data", "published_at" = EXCLUDED."published_at", "feed_id" = EXCLUDED."feed_id"
WHERE "items"."data" IS DISTINCT FROM EXCLUDED."data"
RETURNING (xmax = 0)
`, p.channelID, optFeedID, item.ID, &item, t).Scan(&inserted)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("insert item: %w", err)
	}

	return inserted, nil
}

// MarkRead
func (p *postgresStream) MarkRead(uids []string) error {
	ctx := context.Background()
	conn, err := p.database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("getting connection: %w", err)
	}
	defer conn.Close()
	_, err = conn.ExecContext(context.Background(), `UPDATE "items" SET is_read = 1 WHERE "uid" = ANY($1)`, pq.Array(uids))
	if err != nil {
		return fmt.Errorf("while marking as read: %w", err)
	}
	return nil
}

// Item returns the item with id for this channel
func (p *postgresStream) ItemsByUID(uids []string) ([]microsub.Item, error) {

	var items []microsub.Item

	for _, uid := range uids {
		var item microsub.Item
		var createdAt time.Time
		var isRead int
		var publishedAt time.Time

		row := p.database.QueryRow(`
			SELECT  "data", "created_at", "is_read", "published_at"
			FROM "items"
			WHERE "uid" = $1
		`, uid)

		err := row.Scan(&item, &createdAt, &isRead, &publishedAt)
		if err == sql.ErrNoRows {
			return nil, ErrItemNotFound
		} else if err != nil {
			log.Println("Scan failed", err)
			return nil, err
		}

		item.Read = isRead == 1
		item.ID = uid
		// Same canonical form as Items(): clients parse RFC3339, not the Postgres text.
		item.Published = publishedAt.UTC().Format(time.RFC3339)
		items = append(items, item)
	}

	return items, nil
}
