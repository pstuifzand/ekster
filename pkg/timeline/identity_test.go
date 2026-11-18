package timeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The uid column carries one unique index, so an item written with an empty id
// took the row every other unidentified item needed too. Feed notifications
// carry no id, and every failure overwrote the previous notification.
func TestItemIdentityGivesAnUnidentifiedItemItsOwnUID(t *testing.T) {
	first := itemIdentity("notifications", "")
	second := itemIdentity("notifications", "")

	assert.NotEmpty(t, first, "an empty uid is the collision")
	assert.NotEqual(t, first, second, "two unidentified items must not share a row")
	assert.Regexp(t, `^[0-9a-f]{64}$`, first)
}

// A caller that names its item keeps that name: the uid is what makes a
// re-fetched entry update the row it already has instead of adding another.
func TestItemIdentityKeepsTheIDItIsGiven(t *testing.T) {
	assert.Equal(t, "68747470733a2f2f", itemIdentity("news", "68747470733a2f2f"))
}
