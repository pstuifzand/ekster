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
