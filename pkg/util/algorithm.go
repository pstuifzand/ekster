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

package util

import "reflect"

// Rotate rotates values of an array, between index f and l with midpoint k
func Rotate(a interface{}, f, k, l int) int {
	swapper := reflect.Swapper(a)
	if f == k {
		return l
	}
	if k == l {
		return f
	}
	next := k

	for {
		swapper(f, next)
		f++
		next++
		if f == k {
			k = next
		}
		if next == l {
			break
		}
	}

	ret := f
	for next = k; next != l; {
		swapper(f, next)
		f++
		next++
		if f == k {
			k = next
		} else if next == l {
			next = k
		}
	}
	return ret
}

// StablePartition partitions elements of the array between indices f and l according to predicate p
func StablePartition(a interface{}, f, l int, p func(i int) bool) int {
	n := l - f

	if n == 0 {
		return f
	}

	if n == 1 {
		t := f
		if p(f) {
			t++
		}
		return t
	}

	m := f + (n / 2)

	return Rotate(a, StablePartition(a, f, m, p), m, StablePartition(a, m, l, p))
}
