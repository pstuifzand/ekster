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

package microsub

import "errors"

// ErrFeedAlreadyFollowed is returned by FollowURL when a feed URL is already
// followed, but in a different channel. A feed row belongs to exactly one
// channel ("feeds"."url" is unique), so following it a second time must not
// insert a duplicate row or steal the feed from the channel that owns it: the
// handler answers 409 Conflict instead of a 500 carrying the constraint error.
var ErrFeedAlreadyFollowed = errors.New("feed is already followed in another channel")
