package microsub

import "errors"

// ErrInvalidChannelOrder indicates an unknown or duplicate channel UID.
var ErrInvalidChannelOrder = errors.New("invalid channel order")
