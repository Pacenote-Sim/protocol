package trace

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pacenote-sim/protocol/wire"
)

// update regenerates testdata/golden and the FuzzDecode seed corpus from the
// generators in vectors_test.go. It is not something to reach for: see
// testdata/README.md for what regenerating these files means.
var update = flag.Bool("update", false, "rewrite the golden vectors and the fuzz seed corpus")

const goldenDir = "testdata/golden"

// TestGolden is the contract. The .json files are the inputs, the .bin files are
// the bytes this codec must produce from them, and both are committed, because
// the client and the server are built in separate repositories and are each
// tested against these same files without either one being able to see the
// other's code.
//
// Three things are asserted per vector, and they are different things:
//
//   - Encode produces exactly the committed bytes. This is the one that breaks
//     when the encoder changes, including when it changes for a reason as
//     innocent as a zstd upgrade.
//   - Decode of the committed bytes produces exactly the committed input. This
//     is the one that must never break, because old blobs are in a database.
//   - The blob's version byte is the version this build writes.
func TestGolden(t *testing.T) {
	t.Parallel()

	names := goldenNames(t)
	for _, c := range goldenCases {
		require.Contains(t, names, c.name,
			"required vector %q is missing from %s; run: go test ./trace -update", c.name, goldenDir)
	}
	require.Len(t, names, len(goldenCases), "testdata/golden holds a vector with no case in goldenCases")

	for _, c := range goldenCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			want := readTrace(t, filepath.Join(goldenDir, c.name+".json"))
			wantBin, err := os.ReadFile(filepath.Join(goldenDir, c.name+".bin"))
			r.NoError(err)

			gotBin, err := Encode(want)
			r.NoError(err, "the committed input must be encodable: %s", c.why)
			r.True(bytes.Equal(wantBin, gotBin),
				"encoded bytes differ from %s.bin (%d committed, %d produced); "+
					"if this is intended, read testdata/README.md before running -update",
				c.name, len(wantBin), len(gotBin))

			v, err := Version(wantBin)
			r.NoError(err)
			r.Equal(CodecVersion, v)

			got, err := Decode(wantBin, nil)
			r.NoError(err)
			equalTrace(t, want, got, "the committed bytes must decode to the committed input")
		})
	}
}

// TestGoldenDecodesIntoAReusedBuffer is the golden set run through the path the
// server actually uses: one buffer, reused for every lap. It is separate from
// TestGolden because the thing it can catch — a decode that reads a stale point
// out of the buffer instead of writing a fresh one — only shows up when the
// buffer arrives dirty.
func TestGoldenDecodesIntoAReusedBuffer(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	dst := make([]wire.TracePoint, 0, MaxPoints)
	for _, c := range goldenCases {
		want := readTrace(t, filepath.Join(goldenDir, c.name+".json"))
		blob, err := os.ReadFile(filepath.Join(goldenDir, c.name+".bin"))
		r.NoError(err)

		dst, err = Decode(blob, dst)
		r.NoError(err, c.name)
		equalTrace(t, want, dst, c.name)
	}
}

func goldenNames(t *testing.T) []string {
	t.Helper()
	jsons, err := filepath.Glob(filepath.Join(goldenDir, "*.json"))
	require.NoError(t, err)
	bins, err := filepath.Glob(filepath.Join(goldenDir, "*.bin"))
	require.NoError(t, err)
	require.Len(t, bins, len(jsons), "every golden input needs its encoded twin")

	names := make([]string, 0, len(jsons))
	for _, p := range jsons {
		names = append(names, strings.TrimSuffix(filepath.Base(p), ".json"))
	}
	sort.Strings(names)
	return names
}

// equalTrace compares two traces sample by sample. It exists because a trace of
// no samples is legitimately either a nil slice or an empty one — encoding/json
// produces the second and Decode produces the first — and the difference is not
// one the wire format has, so a test must not invent it.
func equalTrace(t *testing.T, want, got []wire.TracePoint, msg string) {
	t.Helper()
	r := require.New(t)
	r.Len(got, len(want), msg)
	for i := range want {
		r.Equalf(want[i], got[i], "%s: point %d", msg, i)
	}
}

func readTrace(t *testing.T, path string) []wire.TracePoint {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var pts []wire.TracePoint
	require.NoError(t, json.Unmarshal(b, &pts), path)
	return pts
}

// writeGolden rewrites every vector and the fuzz seed corpus. It is reached only
// through -update.
func writeGolden() error {
	if err := os.MkdirAll(goldenDir, 0o755); err != nil {
		return fmt.Errorf("golden dir: %w", err)
	}
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		return fmt.Errorf("corpus dir: %w", err)
	}
	for _, c := range goldenCases {
		pts := c.points()
		blob, err := Encode(pts)
		if err != nil {
			return fmt.Errorf("%s: %w", c.name, err)
		}
		if err := os.WriteFile(filepath.Join(goldenDir, c.name+".json"), marshalTrace(pts), 0o644); err != nil {
			return fmt.Errorf("%s.json: %w", c.name, err)
		}
		if err := os.WriteFile(filepath.Join(goldenDir, c.name+".bin"), blob, 0o644); err != nil {
			return fmt.Errorf("%s.bin: %w", c.name, err)
		}
	}
	return writeCorpus()
}

// marshalTrace writes a trace as JSON with one sample per line. It is the same
// document encoding/json would produce, laid out so that a diff of a golden
// input names the sample that changed instead of colouring the whole file.
func marshalTrace(pts []wire.TracePoint) []byte {
	if len(pts) == 0 {
		return []byte("[]\n")
	}
	var buf bytes.Buffer
	buf.WriteString("[\n")
	for i, p := range pts {
		b, err := json.Marshal(p)
		if err != nil { // unreachable: TracePoint is twelve ints
			panic(err)
		}
		buf.Write(b)
		if i < len(pts)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("]\n")
	return buf.Bytes()
}

// writeCorpus seeds FuzzDecode from the golden vectors plus a set of deliberate
// corruptions of them, so that the fuzzer starts from bytes that get past the
// header and into the parser rather than having to discover a valid header by
// mutation.
func writeCorpus() error {
	seeds, err := corpusSeeds()
	if err != nil {
		return err
	}
	old, err := filepath.Glob(filepath.Join(corpusDir, "*"))
	if err != nil {
		return fmt.Errorf("corpus glob: %w", err)
	}
	for _, p := range old {
		if err := os.Remove(p); err != nil {
			return fmt.Errorf("corpus clean: %w", err)
		}
	}
	for name, seed := range seeds {
		body := "go test fuzz v1\n[]byte(" + strconv.Quote(string(seed)) + ")\n"
		if err := os.WriteFile(filepath.Join(corpusDir, name), []byte(body), 0o644); err != nil {
			return fmt.Errorf("corpus %s: %w", name, err)
		}
	}
	return nil
}
