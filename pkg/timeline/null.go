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

package timeline

import (
	"errors"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

// ErrItemNotFound is an error for when an item is not found
var ErrItemNotFound = errors.New("Item not found")

type nullTimeline struct {
	channel string
}

func (timeline *nullTimeline) Init() error {
	return nil
}

func (timeline *nullTimeline) Items(before, after string) (microsub.Timeline, error) {
	return microsub.Timeline{Items: []microsub.Item{}}, nil
}

func (timeline *nullTimeline) AddItem(item microsub.Item) (bool, error) {
	return false, nil
}

func (timeline *nullTimeline) Count() (int, error) {
	return 0, nil
}

func (timeline *nullTimeline) MarkRead(uids []string) error {
	return nil
}
func (timeline *nullTimeline) ItemsByUID(uid []string) ([]microsub.Item, error) {
	return nil, ErrItemNotFound
}
