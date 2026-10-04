package memory_test

import (
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/conformance"
	"github.com/skosovsky/memy/store/memory"
)

func TestConformance(t *testing.T) {
	conformance.StoreSuite(t, func(*testing.T) memy.Store { return memory.New() })
}
