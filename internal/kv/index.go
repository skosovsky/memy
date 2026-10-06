package kv

import "github.com/skosovsky/memy/internal/workcost"

const maxPrefixByte = 255

// Index is a scope-owned AVL index of live keys. Tombstones retain CAS versions
// in the value map but do not make prefix reads scan deleted or unrelated keys.
type Index struct{ root *node }

type node struct {
	key         string
	left, right *node
	height      int
}

func height(n *node) int {
	if n == nil {
		return 0
	}
	return n.height
}
func refresh(n *node) { n.height = 1 + max(height(n.left), height(n.right)) }
func left(n *node) *node {
	owned := *n
	n = &owned
	pivot := *n.right
	p := &pivot
	n.right = p.left
	p.left = n
	refresh(n)
	refresh(p)
	return p
}
func right(n *node) *node {
	owned := *n
	n = &owned
	pivot := *n.left
	p := &pivot
	n.left = p.right
	p.right = n
	refresh(n)
	refresh(p)
	return p
}
func balance(n *node) *node {
	if n == nil {
		return nil
	}
	refresh(n)
	if height(n.left)-height(n.right) > 1 {
		if height(n.left.left) < height(n.left.right) {
			n.left = left(n.left)
		}
		return right(n)
	}
	if height(n.right)-height(n.left) > 1 {
		if height(n.right.right) < height(n.right.left) {
			n.right = right(n.right)
		}
		return left(n)
	}
	return n
}
func insert(n *node, key string) *node {
	if n == nil {
		return &node{key: key, height: 1, left: nil, right: nil}
	}
	owned := *n
	n = &owned
	switch {
	case key < n.key:
		n.left = insert(n.left, key)
	case key > n.key:
		n.right = insert(n.right, key)
	default:
		return n
	}
	return balance(n)
}
func remove(n *node, key string) *node {
	if n == nil {
		return nil
	}
	owned := *n
	n = &owned
	switch {
	case key < n.key:
		n.left = remove(n.left, key)
	case key > n.key:
		n.right = remove(n.right, key)
	default:
		if n.left == nil {
			return n.right
		}
		if n.right == nil {
			return n.left
		}
		successor := n.right
		for successor.left != nil {
			successor = successor.left
		}
		n.key = successor.key
		n.right = remove(n.right, successor.key)
	}
	return balance(n)
}

func (i *Index) Add(key string)    { i.root = insert(i.root, key) }
func (i *Index) Remove(key string) { i.root = remove(i.root, key) }

func upperPrefix(prefix string) string {
	end := []byte(prefix)
	for j := len(end) - 1; j >= 0; j-- {
		if end[j] != maxPrefixByte {
			end[j]++
			return string(end[:j+1])
		}
	}
	return ""
}

// Keys visits only the ordered prefix range after an exclusive key. A zero
// limit requests the complete selected prefix, never the complete scope first.
func (i *Index) Keys(prefix, after string, limit int) []string {
	keys := make([]string, 0)
	upper := upperPrefix(prefix)
	var visit func(*node)
	visit = func(n *node) {
		if n == nil || (limit > 0 && len(keys) >= limit) {
			return
		}
		workcost.Node()
		if n.key < prefix || n.key <= after {
			visit(n.right)
			return
		}
		if upper != "" && n.key >= upper {
			visit(n.left)
			return
		}
		visit(n.left)
		if limit == 0 || len(keys) < limit {
			keys = append(keys, n.key)
		}
		visit(n.right)
	}
	visit(i.root)
	return keys
}
