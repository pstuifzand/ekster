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

// Items
func (p *postgresStream) Items(before, after string) (microsub.Timeline, error) {
	ctx := context.Background()
	conn, err := p.database.Conn(ctx)
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
	if before != "" {
		b, err := time.Parse(time.RFC3339, before)
		if err != nil {
			log.Println(err)
		} else {
			args = append(args, b)
			qb.WriteString(` AND "published_at" > $2`)
		}
	} else if after != "" {
		b, err := time.Parse(time.RFC3339, after)
		if err == nil {
			args = append(args, b)
			qb.WriteString(` AND "published_at" < $2`)
		}
	}
	qb.WriteString(` ORDER BY "published_at" DESC LIMIT 20`)

	rows, err := conn.QueryContext(context.Background(), qb.String(), args...)
	if err != nil {
		return microsub.Timeline{}, fmt.Errorf("while query: %w", err)
	}

	var tl microsub.Timeline

	var first, last string

	for rows.Next() {
		var id int
		var uid string
		var item microsub.Item
		var createdAt time.Time
		var isRead int
		var publishedAt string

		err = rows.Scan(&id, &uid, &item, &createdAt, &isRead, &publishedAt)
		if err != nil {
			break
		}
		if first == "" {
			first = publishedAt
		}
		last = publishedAt

		item.Read = isRead == 1
		item.ID = uid
		item.Published = publishedAt

		tl.Items = append(tl.Items, item)
	}
	if closeErr := rows.Close(); closeErr != nil {
		return tl, err
	}
	if err != nil {
		return tl, err
	}
	if err = rows.Err(); err != nil {
		return tl, err
	}

	if len(tl.Items) > 0 && p.hasMoreBefore(conn, tl.Items[0].Published) {
		tl.Paging.Before = tl.Items[0].Published
	}
	if p.hasMoreAfter(conn, last) {
		tl.Paging.After = last
	}

	if tl.Items == nil {
		tl.Items = []microsub.Item{}
	}

	return tl, nil
}

// hasMoreBefore reports whether the channel has items older than the first item
// on the page. The channel is part of the WHERE: counting the whole table made
// the answer depend on other channels, and made every page pay for a full scan.
func (p *postgresStream) hasMoreBefore(conn *sql.Conn, before string) bool {
	row := conn.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM "items" WHERE "channel_id" = $1 AND "published_at" > $2 LIMIT 1`, p.channelID, before)
	var count int
	if err := row.Scan(&count); err == sql.ErrNoRows {
		return false
	}
	return count > 0
}

// hasMoreAfter reports whether the channel has items newer than the last item
// on the page, scoped to the channel for the same reason as hasMoreBefore.
func (p *postgresStream) hasMoreAfter(conn *sql.Conn, after string) bool {
	if after == "" {
		return false
	}
	row := conn.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM "items" WHERE "channel_id" = $1 AND "published_at" < $2`, p.channelID, after)
	var count int
	if err := row.Scan(&count); err == sql.ErrNoRows {
		return false
	}
	return count > 0
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
		var publishedAt string

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
		item.Published = publishedAt
		items = append(items, item)
	}

	return items, nil
}
