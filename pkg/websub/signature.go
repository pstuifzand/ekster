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
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/pkg/errors"
)

// ValidateHubSignature validate a sha1 signature that could be send with the
// hub as an extra header
func ValidateHubSignature(sig string, feedContent, secret []byte) error {
	parts := strings.Split(sig, "=")

	if len(parts) != 2 {
		return errors.New("signature format is not like sha1=signature")
	}

	if parts[0] != "sha1" {
		return errors.New("signature format is not like sha1=signature")
	}

	// verification
	mac := hmac.New(sha1.New, secret)
	mac.Write(feedContent)
	signature := mac.Sum(nil)

	signature2, err := hex.DecodeString(parts[1])
	if err != nil {
		return errors.Wrap(err, "could not decode signature")
	}

	if !hmac.Equal(signature, signature2) {
		return fmt.Errorf("signature does not match feed %s %s", signature, parts[1])
	}

	return nil
}
