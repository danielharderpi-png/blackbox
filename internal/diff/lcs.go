package diff

// maxEditDistance bounds the memory used by the Myers trace (O(D^2)). Beyond
// it, an approximate multiset comparison is used instead.
const maxEditDistance = 4000

// Lines computes a minimal line diff (Myers O(ND)) between a and b. It returns
// the indices of lines added in b and removed from a. approx is true when the
// edit distance exceeded maxEditDistance and a multiset approximation was used.
func Lines(a, b []string) (added, removed []int, approx bool) {
	// Trim common prefix and suffix.
	lo := 0
	for lo < len(a) && lo < len(b) && a[lo] == b[lo] {
		lo++
	}
	ea, eb := len(a), len(b)
	for ea > lo && eb > lo && a[ea-1] == b[eb-1] {
		ea--
		eb--
	}
	ma, mb := a[lo:ea], b[lo:eb]
	switch {
	case len(ma) == 0:
		for i := range mb {
			added = append(added, lo+i)
		}
		return added, nil, false
	case len(mb) == 0:
		for i := range ma {
			removed = append(removed, lo+i)
		}
		return nil, removed, false
	}
	ad, rm, ok := myers(ma, mb)
	if !ok {
		ad, rm = multiset(ma, mb)
		approx = true
	}
	for _, i := range ad {
		added = append(added, lo+i)
	}
	for _, i := range rm {
		removed = append(removed, lo+i)
	}
	return added, removed, approx
}

func myers(a, b []string) (added, removed []int, ok bool) {
	n, m := len(a), len(b)
	max := n + m
	if max > maxEditDistance {
		max = maxEditDistance
	}
	off := max + 1
	v := make([]int, 2*max+3)
	// trace[d] holds v[-d-1 .. d+1] as it was at the start of round d.
	var trace [][]int
	found := -1
	for d := 0; d <= max && found < 0; d++ {
		snap := make([]int, 2*d+3)
		copy(snap, v[off-d-1:off+d+2])
		trace = append(trace, snap)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				found = d
				break
			}
		}
	}
	if found < 0 {
		return nil, nil, false
	}
	x, y := n, m
	for d := found; d >= 0; d-- {
		tr := trace[d]
		get := func(k int) int { return tr[k+d+1] }
		k := x - y
		var prevK int
		if k == -d || (k != d && get(k-1) < get(k+1)) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := get(prevK)
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			x--
			y--
		}
		if d > 0 {
			if x == prevX {
				added = append(added, prevY) // moved down: b[prevY] inserted
			} else {
				removed = append(removed, prevX) // moved right: a[prevX] deleted
			}
		}
		x, y = prevX, prevY
	}
	reverse(added)
	reverse(removed)
	return added, removed, true
}

func reverse(s []int) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// multiset approximates a diff by counting line occurrences (ignores order).
func multiset(a, b []string) (added, removed []int) {
	count := map[string]int{}
	for _, l := range a {
		count[l]++
	}
	for i, l := range b {
		if count[l] > 0 {
			count[l]--
		} else {
			added = append(added, i)
		}
	}
	count = map[string]int{}
	for _, l := range b {
		count[l]++
	}
	for i, l := range a {
		if count[l] > 0 {
			count[l]--
		} else {
			removed = append(removed, i)
		}
	}
	return added, removed
}
