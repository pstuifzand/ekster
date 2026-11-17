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

// A thread item outside the user's channels has no _id, so it must not carry a
// read state either: there is no channel in which it could have been read.
func Test_MarshalThreadHidesStateOutsideChannels(t *testing.T) {
	thread := Thread{
		Items: []Item{
			{Type: "entry", UID: "https://ann.example/root", URL: "https://ann.example/root"},
			{Type: "entry", ID: "b3d1", UID: "https://bob.example/re", URL: "https://bob.example/re", Read: true},
		},
		Thread: ThreadInfo{Root: "https://ann.example/root", Partial: true, Missing: 2},
	}

	bytes, err := json.Marshal(thread)
	if !assert.NoError(t, err) {
		return
	}

	var got struct {
		Items  []map[string]interface{} `json:"items"`
		Thread ThreadInfo               `json:"thread"`
	}
	if !assert.NoError(t, json.Unmarshal(bytes, &got)) {
		return
	}
	if !assert.Len(t, got.Items, 2) {
		return
	}

	_, hasID := got.Items[0]["_id"]
	_, hasRead := got.Items[0]["_is_read"]
	assert.False(t, hasID, "an entry outside the channels must not carry an _id")
	assert.False(t, hasRead, "an entry without an _id must not claim a read state")

	assert.Equal(t, "b3d1", got.Items[1]["_id"])
	assert.Equal(t, true, got.Items[1]["_is_read"])
	assert.True(t, got.Thread.Partial)
	assert.Equal(t, 2, got.Thread.Missing)
}

// A thread that finds nothing is an empty list, not null: the client renders
// what it gets, and null is a shape it has to guard against.
func Test_MarshalThreadEmptyItemsIsArray(t *testing.T) {
	bytes, err := json.Marshal(Thread{Items: []Item{}})
	if !assert.NoError(t, err) {
		return
	}
	assert.Contains(t, string(bytes), `"items":[]`)
}

// A reference is not the conversation, so it lives outside Items — and it gets
// the same treatment: no _id, no read state, because there is no channel in
// which it could have been read.
func Test_MarshalThreadCarriesReferences(t *testing.T) {
	thread := Thread{
		Items: []Item{{Type: "entry", ID: "a1", URL: "https://jeremy.example/compass"}},
		Refs: map[string]Item{
			"https://david.example/compass": {Type: "entry", URL: "https://david.example/compass", Name: "Thinking about compass"},
		},
		Thread: ThreadInfo{Root: "https://jeremy.example/compass"},
	}

	bytes, err := json.Marshal(thread)
	if !assert.NoError(t, err) {
		return
	}

	var got struct {
		Items []map[string]interface{}          `json:"items"`
		Refs  map[string]map[string]interface{} `json:"refs"`
	}
	if !assert.NoError(t, json.Unmarshal(bytes, &got)) {
		return
	}

	ref, ok := got.Refs["https://david.example/compass"]
	if !assert.True(t, ok, "the reference must be in refs") {
		return
	}
	_, hasRead := ref["_is_read"]
	assert.False(t, hasRead, "a reference outside the channels must not claim a read state")
	assert.Equal(t, "Thinking about compass", ref["name"])
	assert.Len(t, got.Items, 1)
}

// No references is an absent field, not an empty object.
func Test_MarshalThreadWithoutReferences(t *testing.T) {
	bytes, err := json.Marshal(Thread{Items: []Item{{Type: "entry", ID: "a1"}}})
	if !assert.NoError(t, err) {
		return
	}
	assert.NotContains(t, string(bytes), `"refs"`)
}
