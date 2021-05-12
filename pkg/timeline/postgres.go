package timeline

import (
	"database/sql"
	"fmt"
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
	db := p.database
	err := db.Ping()
	if err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS "channels" (
    "id" int primary key generated always as identity,
    "name" varchar(255) unique,
    "created_at" timestamp DEFAULT current_timestamp
);
`)
	if err != nil {
		return fmt.Errorf("create channels table failed: %w", err)
	}

	_, err = db.Exec(`
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

	_, err = db.Exec(`INSERT INTO "channels" ("name", "created_at") VALUES ($1, DEFAULT)
 		ON CONFLICT DO NOTHING`, p.channel)
	if err != nil {
		return fmt.Errorf("create channel failed: %w", err)
	}

	rows, err := db.Query(`SELECT "id" FROM "channels" WHERE "name" = $1`, p.channel)
	if err != nil {
		return fmt.Errorf("fetch channel failed: %w", err)
	}
	for rows.Next() {
		err = rows.Scan(&p.channelID)
		if err != nil {
			return fmt.Errorf("fetch channel failed while scanning: %w", err)
		}
		break
	}

	return nil
}

// Items
func (p *postgresStream) Items(before, after string) (microsub.Timeline, error) {
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
		if err == nil {
			args = append(args, b)
			qb.WriteString(` AND "published_at" < $2`)
		}
	}
	qb.WriteString(` ORDER BY "published_at"`)

	rows, err := p.database.Query(qb.String(), args...)
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

	tl.Paging.Before = first
	tl.Paging.After = last

	return tl, nil
}

// Count
func (p *postgresStream) Count() (int, error) {
	rows, err := p.database.Query("SELECT COUNT(*) FROM items WHERE channel_id = ?", p.channel)
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
	t, err := time.Parse("2006-01-02T15:04:05Z0700", item.Published)
	if err != nil {
		t2, err := time.Parse("2006-01-02T15:04:05Z07:00", item.Published)
		if err != nil {
			return false, fmt.Errorf("while adding item: time %q could not be parsed: %w", item.Published, err)
		}
		t = t2
	}

	_, err = p.database.Exec(`
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
	_, err := p.database.Exec(`UPDATE "items" SET is_read = 1 WHERE "uid" IN ($1)`, uids)
	if err != nil {
		return fmt.Errorf("while marking as read: %w", err)
	}
	return nil
}
