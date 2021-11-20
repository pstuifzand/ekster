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

package microsub

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_UnmarshalUnreadBool(t *testing.T) {
	var x Unread
	err := json.Unmarshal([]byte("false"), &x)
	if assert.NoError(t, err) {
		assert.Equal(t, UnreadBool, x.Type)
		assert.False(t, x.Unread)
	}
}

func Test_UnmarshalUnreadBoolTrue(t *testing.T) {
	var x Unread
	err := json.Unmarshal([]byte("true"), &x)
	if assert.NoError(t, err) {
		assert.Equal(t, UnreadBool, x.Type)
		assert.True(t, x.Unread)
	}
}

func Test_UnmarshalUnreadIntZero(t *testing.T) {
	var x Unread
	err := json.Unmarshal([]byte("0"), &x)
	if assert.NoError(t, err) {
		assert.Equal(t, UnreadCount, x.Type)
		assert.Equal(t, 0, x.UnreadCount)
	}
}

func Test_UnmarshalUnreadIntNonZero(t *testing.T) {
	var x Unread
	err := json.Unmarshal([]byte("209449"), &x)
	if assert.NoError(t, err) {
		assert.Equal(t, UnreadCount, x.Type)
		assert.Equal(t, 209449, x.UnreadCount)
	}
}

func Test_MarshalUnreadEmpty(t *testing.T) {
	x := Unread{}
	bytes, err := json.Marshal(x)
	if assert.NoError(t, err) {
		assert.Equal(t, "false", string(bytes))
	}
}

func Test_MarshalUnreadBoolFalse(t *testing.T) {
	x := Unread{Type: UnreadBool, Unread: false}
	bytes, err := json.Marshal(x)
	if assert.NoError(t, err) {
		assert.Equal(t, "false", string(bytes))
	}
}

func Test_MarshalUnreadBoolTrue(t *testing.T) {
	x := Unread{Type: UnreadBool, Unread: true}
	bytes, err := json.Marshal(x)
	if assert.NoError(t, err) {
		assert.Equal(t, "true", string(bytes))
	}
}

func Test_MarshalUnreadIntZero(t *testing.T) {
	x := Unread{Type: UnreadCount, UnreadCount: 0}
	bytes, err := json.Marshal(x)
	if assert.NoError(t, err) {
		assert.Equal(t, "0", string(bytes))
	}
}

func Test_MarshalUnreadIntNonZero(t *testing.T) {
	x := Unread{Type: UnreadCount, UnreadCount: 1884844}
	bytes, err := json.Marshal(x)
	if assert.NoError(t, err) {
		assert.Equal(t, "1884844", string(bytes))
	}
}
