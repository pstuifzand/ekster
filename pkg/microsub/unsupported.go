package microsub

import (
	"context"
	"errors"
)

// ErrUnsupported indicates a protocol operation is not implemented yet.
var ErrUnsupported = errors.New("microsub operation not supported")

// UnsupportedMethods supplies explicit failures for protocol operations that
// an implementation has not yet added.
type UnsupportedMethods struct{}

func (UnsupportedMethods) ChannelsOrder(context.Context, []string) error { return ErrUnsupported }
func (UnsupportedMethods) TimelineGetWithLimit(context.Context, string, string, string, int) (Timeline, error) {
	return Timeline{}, ErrUnsupported
}
func (UnsupportedMethods) MarkUnread(context.Context, string, []string) error { return ErrUnsupported }
func (UnsupportedMethods) MarkReadThrough(context.Context, string, string) error {
	return ErrUnsupported
}
func (UnsupportedMethods) TimelineRemove(context.Context, string, []string) error {
	return ErrUnsupported
}
func (UnsupportedMethods) ItemSearchWithOptions(context.Context, ItemSearchOptions) (Timeline, error) {
	return Timeline{}, ErrUnsupported
}
func (UnsupportedMethods) MuteGetList(context.Context, string) ([]string, error) {
	return nil, ErrUnsupported
}
func (UnsupportedMethods) MuteURL(context.Context, string, string) error   { return ErrUnsupported }
func (UnsupportedMethods) UnmuteURL(context.Context, string, string) error { return ErrUnsupported }
func (UnsupportedMethods) BlockGetList(context.Context, string) ([]Card, error) {
	return nil, ErrUnsupported
}
func (UnsupportedMethods) BlockURL(context.Context, string, string) error   { return ErrUnsupported }
func (UnsupportedMethods) UnblockURL(context.Context, string, string) error { return ErrUnsupported }
