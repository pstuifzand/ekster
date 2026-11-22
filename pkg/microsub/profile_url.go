package microsub

import (
	"fmt"
	"net/url"
)

// ValidateProfileURL accepts the profile URLs that can name an author to mute
// or block: an absolute http(s) URL with a host and no fragment. A fragment
// never identifies a different person, and anything else cannot be compared
// with the author URL stored in an item.
func ValidateProfileURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Fragment != "" {
		return fmt.Errorf("%w: invalid profile URL", ErrInvalidProfileURL)
	}
	return nil
}
