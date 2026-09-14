package wire

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package if a test leaves a goroutine behind. This package
// starts none — it is types and string constants — and the check is here so that
// it stays that way.
func TestMain(m *testing.M) {
	// -update rewrites testdata/shapes.json before anything reads it, for the
	// same reason the trace package does: the tests that read it run in
	// parallel with the one that would be writing it.
	flag.Parse()
	if *update {
		if err := writeShapes(); err != nil {
			fmt.Fprintln(os.Stderr, "regenerating testdata/shapes.json:", err)
			os.Exit(1)
		}
	}
	goleak.VerifyTestMain(m)
}
