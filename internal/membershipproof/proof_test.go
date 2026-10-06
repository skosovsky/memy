package membershipproof

import (
	"fmt"
	"testing"
)

func TestProofBindsEveryRelationAndTreeShape(t *testing.T) {
	for _, count := range []int{1, 2, 3, 7, 16, 31, 100} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			// Arrange: unique exact canonical relation statements, including odd tree sizes.
			bindings := make([]Binding, count)
			for i := range bindings {
				bindings[i] = Binding{Scope: "scope", RecordID: "child", Revision: 3, Relation: "lineage", MatchID: fmt.Sprint(i)}
			}
			// Act: build a counted tree and per-edge witnesses.
			root, proofs := Build(bindings)
			// Assert: every proof validates only its exact binding and immutable shape.
			for i, b := range bindings {
				if !Verify(b, proofs[i], root) {
					t.Fatal("valid proof rejected")
				}
				changed := b
				changed.Revision++
				if Verify(changed, proofs[i], root) {
					t.Fatal("foreign revision accepted")
				}
				p := proofs[i]
				p.Count++
				if Verify(b, p, root) {
					t.Fatal("false count accepted")
				}
				p = proofs[i]
				p.Siblings = append(p.Siblings, "00")
				if Verify(b, p, root) {
					t.Fatal("extra sibling accepted")
				}
			}
		})
	}
}
