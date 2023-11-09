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

	"p83.nl/go/ekster/pkg/microsub"

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
	if err == sql.ErrNoRows {
		return fmt.Errorf("channel %s not found: %w", p.channel, err)
	}

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

	if len(tl.Items) > 0 && hasMoreBefore(conn, tl.Items[0].Published) {
		tl.Paging.Before = tl.Items[0].Published
	}
	if hasMoreAfter(conn, last) {
		tl.Paging.After = last
	}

	if tl.Items == nil {
		tl.Items = []microsub.Item{}
	}

	return tl, nil
}

func hasMoreBefore(conn *sql.Conn, before string) bool {
	row := conn.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM "items" WHERE "published_at" > $1`, before)
	var count int
	if err := row.Scan(&count); err == sql.ErrNoRows {
		return false
	}
	return count > 0
}

func hasMoreAfter(conn *sql.Conn, after string) bool {
	row := conn.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM "items" WHERE "published_at" < $1`, after)
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

// AddItem
func (p *postgresStream) AddItem(item microsub.Item) (bool, error) {
	ctx := context.Background()
	conn, err := p.database.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	t, err := time.Parse("2006-01-02T15:04:05Z0700", item.Published)
	if err != nil {
		t2, err := time.Parse("2006-01-02T15:04:05Z07:00", item.Published)
		if err != nil {
			return false, fmt.Errorf("while adding item: time %q could not be parsed: %w", item.Published, err)
		}
		t = t2
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

	result, err := conn.ExecContext(context.Background(), `
INSERT INTO "items" ("channel_id", "feed_id", "uid", "data", "published_at", "created_at")
VALUES ($1, $2, $3, $4, $5, DEFAULT)
ON CONFLICT ON CONSTRAINT "items_uid_key" DO NOTHING
`, p.channelID, optFeedID, item.ID, &item, t)
	if err != nil {
		return false, fmt.Errorf("insert item: %w", err)
	}
	c, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return c > 0, nil
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
