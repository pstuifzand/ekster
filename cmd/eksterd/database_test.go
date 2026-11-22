//go:build integration

package main

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetChannelFromAuthorization(t *testing.T) {
	db := integrationDB(t)
	var id int
	require.NoError(t, db.QueryRow(`INSERT INTO channels (uid, name) VALUES ('abcdef', 'Channel') RETURNING id`).Scan(&id))
	_, err := db.Exec(`INSERT INTO sources (channel_id, auth_code) VALUES ($1, '1234')`, id)
	require.NoError(t, err)
	target, err := resolveTarget(httptest.NewRequest("POST", "/micropub?source_id=1234", nil), db, nil)
	require.NoError(t, err)
	require.Equal(t, "abcdef", target.Channel)
	target, err = resolveTarget(httptest.NewRequest("POST", "/micropub?source_id=1111", nil), db, nil)
	require.Error(t, err)
	require.Empty(t, target.Channel)
}
