package timeline

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"p83.nl/go/ekster/pkg/microsub"

	// load pq for postgres
	_ "github.com/lib/pq"
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

	_, err = conn.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS "channels" (
    "id" int primary key generated always as identity,
    "name" varchar(255) unique,
    "created_at" timestamp DEFAULT current_timestamp
);
`)
	if err != nil {
		return fmt.Errorf("create channels table failed: %w", err)
	}

	_, err = conn.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS "items" (
    "id" int primary key generated always as identity,
    "channel_id" int references "channels" on delete cascade,
    "uid" varchar(512) not null unique,
    "is_read" int default 0,
    "data" json,
    "created_at" timestamp DEFAULT current_timestamp,
    "updated_at" timestamp,
    "published_at" timestamp
);
`)
	if err != nil {
		return fmt.Errorf("create items table failed: %w", err)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO "channels" ("name", "created_at") VALUES ($1, DEFAULT)
 		ON CONFLICT DO NOTHING`, p.channel)
	if err != nil {
		return fmt.Errorf("create channel failed: %w", err)
	}

	row := conn.QueryRowContext(ctx, `SELECT "id" FROM "channels" WHERE "name" = $1`, p.channel)
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
	rows, err := conn.QueryContext(context.Background(), "SELECT COUNT(*) FROM items WHERE channel_id = ?", p.channel)
	if err != nil {
		return 0, err
	}

	var count int
	for rows.Next() {
		err = rows.Scan(&count)
		if err != nil {
			return -1, err
		}
		break
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

	_, err = conn.ExecContext(context.Background(), `
INSERT INTO "items" ("channel_id", "uid", "data", "published_at", "created_at")
VALUES ($1, $2, $3, $4, DEFAULT)
ON CONFLICT ON CONSTRAINT "items_uid_key" DO UPDATE SET "updated_at" = now()
`, p.channelID, item.ID, &item, t)
	if err != nil {
		return false, fmt.Errorf("while adding item: %w", err)
	}
	return true, nil
}

// MarkRead
func (p *postgresStream) MarkRead(uids []string) error {
	ctx := context.Background()
	conn, err := p.database.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.ExecContext(context.Background(), `UPDATE "items" SET is_read = 1 WHERE "uid" IN ($1)`, uids)
	if err != nil {
		return fmt.Errorf("while marking as read: %w", err)
	}
	return nil
}
