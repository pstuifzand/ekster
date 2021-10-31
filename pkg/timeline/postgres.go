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
	//
	// 	_, err = conn.ExecContext(ctx, `
	// CREATE TABLE IF NOT EXISTS "channels" (
	//     "id" int primary key generated always as identity,
	//     "name" varchar(255) unique,
	//     "created_at" timestamp DEFAULT current_timestamp
	// );
	// `)
	// 	if err != nil {
	// 		return fmt.Errorf("create channels table failed: %w", err)
	// 	}
	//
	// 	_, err = conn.ExecContext(ctx, `
	// CREATE TABLE IF NOT EXISTS "items" (
	//     "id" int primary key generated always as identity,
	//     "channel_id" int references "channels" on delete cascade,
	//     "uid" varchar(512) not null unique,
	//     "is_read" int default 0,
	//     "data" jsonb,
	//     "created_at" timestamp DEFAULT current_timestamp,
	//     "updated_at" timestamp,
	//     "published_at" timestamp
	// );
	// `)
	// 	if err != nil {
	// 		return fmt.Errorf("create items table failed: %w", err)
	// 	}
	//
	// 	_, err = conn.ExecContext(ctx, `ALTER TABLE "items" ALTER COLUMN "data" TYPE jsonb, ALTER COLUMN "uid"  TYPE varchar(1024)`)
	// 	if err != nil {
	// 		return fmt.Errorf("alter items table failed: %w", err)
	// 	}

	_, err = conn.ExecContext(ctx, `INSERT INTO "channels" ("uid", "name", "created_at") VALUES ($1, $1, DEFAULT)
 		ON CONFLICT DO NOTHING`, p.channel)
	if err != nil {
		return fmt.Errorf("create channel failed: %w", err)
	}

	row := conn.QueryRowContext(ctx, `SELECT "id" FROM "channels" WHERE "uid" = $1`, p.channel)
	if row == nil {
		return fmt.Errorf("fetch channel failed: %w", err)
	}
	err = row.Scan(&p.channelID)
	if err != nil {
		return fmt.Errorf("fetch channel failed while scanning: %w", err)
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
			qb.WriteString(` AND "published_at" < $2`)
		}
	} else if after != "" {
		b, err := time.Parse(time.RFC3339, after)
		if err == nil {
			args = append(args, b)
			qb.WriteString(` AND "published_at" > $2`)
		}
	}
	qb.WriteString(` ORDER BY "published_at" DESC LIMIT 10`)

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

	// TODO: should only be set of there are more items available
	tl.Paging.Before = last
	// tl.Paging.After = last

	if tl.Items == nil {
		tl.Items = []microsub.Item{}
	}

	return tl, nil
}

// Count
func (p *postgresStream) Count() (int, error) {
	ctx := context.Background()
	conn, err := p.database.Conn(ctx)
	if err != nil {
		return -1, err
	}
	defer conn.Close()
	row := conn.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM items WHERE channel_id = $1 AND "is_read" = 0`, p.channelID)
	if row == nil {
		return 0, nil
	}
	var count int
	err = row.Scan(&count)
	if err != nil {
		return -1, err
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
	if item.UID == "" {
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
`, p.channelID, optFeedID, item.UID, &item, t)
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
