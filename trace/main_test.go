package trace

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package if any test leaves a goroutine behind. The codec
// starts none of its own, but it holds a zstd encoder and decoder for the life
// of the process and those are exactly the kind of dependency that quietly
// starts a worker pool, so the check is here to notice if one ever does.
func TestMain(m *testing.M) {
	// -update rewrites testdata before anything reads it. It happens here and
	// not inside a test because the golden tests run in parallel and two of them
	// read the files that a third would be writing.
	flag.Parse()
	if *update {
		if err := writeGolden(); err != nil {
			fmt.Fprintln(os.Stderr, "regenerating the golden vectors:", err)
			os.Exit(1)
		}
	}
	goleak.VerifyTestMain(m)
}
