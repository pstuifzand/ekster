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
