package util

import "reflect"

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

func StablePartition(a interface{}, f, l int, p func(i int) bool) int {
	n := l - f

	if n == 0 {
		return f
	}

	if n == 1 {
		t := f
		if p(f) {
			t += 1
		}
		return t
	}

	m := f + (n / 2)

	return Rotate(a, StablePartition(a, f, m, p), m, StablePartition(a, m, l, p))
}
