package fusionapi

import (
	"cmp"
	"slices"
	"sort"
)

// Small list helpers the reads share: what is kept of a list, once each, cut to a limit, and in what order.

// uniqueInOrder is the values with the repeats left out, the first of each where it was. A list of fewer than two is
// returned as it is.
func uniqueInOrder[T comparable](l []T) []T {
	if len(l) < 2 {
		return l
	}
	seen := make(map[T]bool, len(l))
	out := l[:0:0]
	for _, v := range l {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// distinctSorted is the values once each, in ascending order, in a list of its own; nil when there are none. A caller
// that leaves out empty values does so before.
func distinctSorted[T cmp.Ordered](vals []T) []T {
	if len(vals) == 0 {
		return nil
	}
	out := slices.Clone(vals)
	slices.Sort(out)
	return slices.Compact(out)
}

// cutAt is l cut to its first n values, and whether anything was cut. A read asks its store for one value more than it
// keeps, so that an answer exactly n long is told from one that was cut.
func cutAt[T any](l []T, n int) ([]T, bool) {
	if len(l) > n {
		return l[:n], true
	}
	return l, false
}

// firstOf is at most the first n values of l and, when there were more, more(how many were left out) in place of the
// rest, for a message that names a few. It does not change l.
func firstOf(l []string, n int, more func(rest int) string) []string {
	if len(l) > n {
		return append(l[:n:n], more(len(l)-n))
	}
	return l
}

// SortByPlace orders l by cluster, then namespace, as at gives them: the order the places of an application, and of a
// service, are listed in. Entries of the same place keep the order they came in, so the answer reads the same each time.
func SortByPlace[T any](l []T, at func(T) (cluster, namespace string)) {
	sort.SliceStable(l, func(i, j int) bool {
		ci, ni := at(l[i])
		cj, nj := at(l[j])
		if ci != cj {
			return ci < cj
		}
		return ni < nj
	})
}

// OrEmpty is v, or an empty list where it is nil, so that an answer gives [] and not null.
func OrEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}
