package microsub

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidItemSearch identifies malformed content search options.
var ErrInvalidItemSearch = errors.New("invalid content search")

// ValidateItemSearchOptions validates the common request shape. Zero limit uses the server default.
func ValidateItemSearchOptions(o ItemSearchOptions) error {
	if strings.TrimSpace(o.Channel) == "" {
		return fmt.Errorf("%w: channel is required", ErrInvalidItemSearch)
	}
	if o.Limit < 0 {
		return fmt.Errorf("%w: limit must be positive", ErrInvalidItemSearch)
	}
	has := strings.TrimSpace(o.Query) != ""
	for _, values := range [][]string{o.Author, o.Category, o.Kind, o.Source} {
		for _, value := range values {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%w: empty filter", ErrInvalidItemSearch)
			}
			has = true
		}
	}
	if !has {
		return fmt.Errorf("%w: supply query or a filter", ErrInvalidItemSearch)
	}
	return nil
}
