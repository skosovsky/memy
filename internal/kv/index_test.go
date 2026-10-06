package kv

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

func checkAVL(t *testing.T, n *node, lower, upper string) int {
	t.Helper()
	if n == nil {
		return 0
	}
	if (lower != "" && n.key <= lower) || (upper != "" && n.key >= upper) {
		t.Fatalf("unordered node %q", n.key)
	}
	l, r := checkAVL(t, n.left, lower, n.key), checkAVL(t, n.right, n.key, upper)
	if l-r > 1 || r-l > 1 || n.height != 1+max(l, r) {
		t.Fatalf("unbalanced node %q: %d/%d height=%d", n.key, l, r, n.height)
	}
	return 1 + max(l, r)
}

func TestIndexMaintainsOrderedRangesThroughMutation(t *testing.T) {
	// Arrange: deterministic adversarial insertion/removal order and a map oracle.
	var index Index
	values := make(map[string]bool)
	random := rand.New(rand.NewPCG(17, 31))

	// Act: mutate repeatedly, exercising rotations, duplicate adds and absent deletes.
	for operation := range 10000 {
		key := fmt.Sprintf("prefix-%02d/%05d", random.IntN(10), random.IntN(2000))
		if random.IntN(3) == 0 {
			index.Remove(key)
			delete(values, key)
		} else {
			index.Add(key)
			values[key] = true
		}
		if operation%100 != 0 {
			continue
		}

		// Assert: tree shape remains balanced and range pages equal the sorted oracle.
		checkAVL(t, index.root, "", "")
		prefix := fmt.Sprintf("prefix-%02d/", random.IntN(10))
		after := prefix + "00500"
		var want []string
		for key := range values {
			if strings.HasPrefix(key, prefix) && key > after {
				want = append(want, key)
			}
		}
		slices.Sort(want)
		if len(want) > 7 {
			want = want[:7]
		}
		if got := index.Keys(prefix, after, 7); !slices.Equal(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestIndexSnapshotSurvivesRotationsAndRemovals(t *testing.T) {
	// Arrange: a retained root represents a transaction that may roll back.
	var original Index
	for i := range 1000 {
		original.Add(fmt.Sprintf("key/%04d", i))
	}
	want := original.Keys("key/", "", 0)
	changed := original

	// Act: independently mutate the shared initial tree through many rotations.
	for i := range 1000 {
		changed.Remove(fmt.Sprintf("key/%04d", i))
		changed.Add(fmt.Sprintf("other/%04d", 1000-i))
	}

	// Assert: path copying protects the retained canonical tree and both indexes.
	if got := original.Keys("key/", "", 0); !slices.Equal(got, want) {
		t.Fatal("overlay mutated canonical index")
	}
	checkAVL(t, original.root, "", "")
	checkAVL(t, changed.root, "", "")
	if got := changed.Keys("key/", "", 0); len(got) != 0 {
		t.Fatal("deleted keys remained live")
	}
}
