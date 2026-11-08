package auth

import (
	"encoding/json"
	"testing"
)

// Servers disagree about issued_at, so decoding a token response must not
// depend on which shape they chose.
func TestTimestampAcceptsBothShapes(t *testing.T) {
	cases := map[string]struct {
		body string
		want int64
	}{
		"number": {`{"issued_at": 1700000000}`, 1700000000},
		"string": {`{"issued_at": "2026-11-08T01:05:38Z"}`, 1762563938},
		"empty":  {`{}`, 0},
	}

	for name, c := range cases {
		var got TokenResponse
		if err := json.Unmarshal([]byte(c.body), &got); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if int64(got.IssuedAt) != c.want {
			t.Errorf("%s: IssuedAt = %d, want %d", name, int64(got.IssuedAt), c.want)
		}
	}
}
