package main

import (
	"context"
	"fmt"

	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/userid"
)

// ChannelsOrder permutes only the slots occupied by the supplied channels.
func (b *memoryBackend) ChannelsOrder(ctx context.Context, uids []string) error {
	userID, ok := userid.FromContext(ctx)
	if !ok {
		return fmt.Errorf("missing user identity")
	}
	tx, err := b.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Serialize even when there are initially no channel rows to lock.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(194822, $1)`, userID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT uid FROM channels WHERE user_id = $1 ORDER BY (uid = 'notifications') DESC, sort_order NULLS LAST, id FOR UPDATE`, userID)
	if err != nil {
		return err
	}
	var current []string
	for rows.Next() {
		var uid string
		if err = rows.Scan(&uid); err != nil {
			rows.Close()
			return err
		}
		current = append(current, uid)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	owned := make(map[string]bool, len(current))
	for _, uid := range current {
		owned[uid] = true
	}
	seen := make(map[string]bool, len(uids))
	var desired []string
	for _, uid := range uids {
		if !owned[uid] || seen[uid] {
			return fmt.Errorf("%w: unknown or duplicate channel %q", microsub.ErrInvalidChannelOrder, uid)
		}
		seen[uid] = true
		if uid != "notifications" {
			desired = append(desired, uid)
		}
	}
	n := 0
	for i, uid := range current {
		if uid != "notifications" && seen[uid] {
			current[i] = desired[n]
			n++
		}
	}
	for i, uid := range current {
		if _, err = tx.ExecContext(ctx, `UPDATE channels SET sort_order = $1 WHERE user_id = $2 AND uid = $3`, i, userID, uid); err != nil {
			return err
		}
	}
	return tx.Commit()
}
