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
