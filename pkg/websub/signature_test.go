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

package websub

import (
	"crypto/hmac"
	"crypto/sha1"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateHubSignature(t *testing.T) {
	secret := []byte("this is a test secret")
	feedContent := []byte("hello world")

	mac := hmac.New(sha1.New, secret)
	mac.Write(feedContent)
	signature := mac.Sum(nil)

	err := ValidateHubSignature(fmt.Sprintf("sha1=%x", signature), feedContent, secret)
	assert.NoError(t, err, "error should be nil")
}
