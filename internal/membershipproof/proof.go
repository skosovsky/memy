// Package membershipproof authenticates canonical revision relation membership.
// It is shared only with privileged test corpus builders, not a public host port.
package membershipproof

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
)

type Binding struct {
	Scope, RecordID   string
	Revision          uint64
	Relation, MatchID string
}
type Witness struct {
	Index    uint64   `json:"index"`
	Count    uint64   `json:"count"`
	Siblings []string `json:"siblings"`
}

func leaf(b Binding) [32]byte {
	raw, _ := json.Marshal(b)
	return sha256.Sum256(append([]byte{0}, raw...))
}
func branch(l, r [32]byte) [32]byte {
	var raw [65]byte
	raw[0] = 1
	copy(raw[1:33], l[:])
	copy(raw[33:], r[:])
	return sha256.Sum256(raw[:])
}
func counted(root [32]byte, n uint64) string {
	var raw [41]byte
	raw[0] = 2
	binary.BigEndian.PutUint64(raw[1:9], n)
	copy(raw[9:], root[:])
	h := sha256.Sum256(raw[:])
	return hex.EncodeToString(h[:])
}
func Shape(p Witness) bool {
	if p.Count == 0 || p.Index >= p.Count || p.Siblings == nil || len(p.Siblings) > 64 {
		return false
	}
	n := p.Count
	depth := 0
	for n > 1 {
		depth++
		n = n/2 + n%2
	}
	if depth != len(p.Siblings) {
		return false
	}
	for _, s := range p.Siblings {
		if len(s) != 64 {
			return false
		}
		if _, err := hex.DecodeString(s); err != nil {
			return false
		}
	}
	return true
}

// Build returns a deterministic root and witnesses in input order.
func Build(bindings []Binding) (string, []Witness) {
	if len(bindings) == 0 {
		return "", nil
	}
	levels := [][][32]byte{make([][32]byte, len(bindings))}
	for i, b := range bindings {
		levels[0][i] = leaf(b)
	}
	for len(levels[len(levels)-1]) > 1 {
		prev := levels[len(levels)-1]
		next := make([][32]byte, (len(prev)+1)/2)
		for i := range next {
			r := min(2*i+1, len(prev)-1)
			next[i] = branch(prev[2*i], prev[r])
		}
		levels = append(levels, next)
	}
	proofs := make([]Witness, len(bindings))
	for i := range proofs {
		p := Witness{Index: uint64(i), Count: uint64(len(bindings)), Siblings: []string{}}
		index := i
		for _, level := range levels[:len(levels)-1] {
			sibling := index ^ 1
			if sibling >= len(level) {
				sibling = index
			}
			p.Siblings = append(p.Siblings, hex.EncodeToString(level[sibling][:]))
			index /= 2
		}
		proofs[i] = p
	}
	return counted(levels[len(levels)-1][0], uint64(len(bindings))), proofs
}
func Verify(b Binding, p Witness, root string) bool {
	if !Shape(p) {
		return false
	}
	hash := leaf(b)
	index, count := p.Index, p.Count
	for _, s := range p.Siblings {
		raw, _ := hex.DecodeString(s)
		var sibling [32]byte
		copy(sibling[:], raw)
		if index%2 == 0 {
			if index+1 >= count && sibling != hash {
				return false
			}
			hash = branch(hash, sibling)
		} else {
			hash = branch(sibling, hash)
		}
		index /= 2
		count = count/2 + count%2
	}
	return counted(hash, p.Count) == root
}
