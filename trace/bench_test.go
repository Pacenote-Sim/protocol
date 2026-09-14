package trace

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pacenote-sim/protocol/wire"
)

// The budgets from docs/SERVER.md, "Performance is a requirement":
//
//	Trace encode, 300 samples   < 40 µs, 1 alloc/op
//	Trace decode, 300 samples   < 20 µs, 0 allocs/op, into a reused buffer
//
// Run them with:
//
//	go test ./trace -run XXX -bench 'Encode|Decode' -benchmem -count=6
//
// The one allocation in Encode is the returned slice and there is no way to have
// fewer. The zero in Decode is the whole point of the destination argument: pass
// the same slice back in and the ingest path never touches the heap.

func BenchmarkEncode(b *testing.B) {
	for _, n := range []int{1, 300, MaxPoints} {
		pts := realisticLap(n)
		b.Run(name(n), func(b *testing.B) {
			b.ReportAllocs()
			var blob []byte
			for b.Loop() {
				var err error
				blob, err = Encode(pts)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.SetBytes(int64(len(blob)))
		})
	}
}

func BenchmarkDecode(b *testing.B) {
	for _, n := range []int{1, 300, MaxPoints} {
		pts := realisticLap(n)
		blob, err := Encode(pts)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(name(n), func(b *testing.B) {
			dst := make([]wire.TracePoint, 0, n)
			b.ReportAllocs()
			b.SetBytes(int64(len(blob)))
			for b.Loop() {
				var err error
				dst, err = Decode(blob, dst)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDecodeIntoNil is the cost of not reusing the destination, kept
// alongside the others so the difference the argument makes is a number and not
// a claim.
func BenchmarkDecodeIntoNil(b *testing.B) {
	blob, err := Encode(realisticLap(300))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Decode(blob, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkValidate is the share of both paths that is neither compression nor
// varints. Decode pays it too, on purpose.
func BenchmarkValidate(b *testing.B) {
	pts := realisticLap(300)
	b.ReportAllocs()
	for b.Loop() {
		if err := Validate(pts); err != nil {
			b.Fatal(err)
		}
	}
}

// TestCompressionRatio reports what the format buys against the JSON it
// replaces, on the realistic lap and on the rest of the golden set.
//
// It asserts a floor and prints everything, because a ratio is a measurement
// and not a requirement, and a floor chosen to match a measurement would only
// be testing the fixture. The floor is 12×; the realistic lap measures a little
// under 15× and decision D-1 of docs/SERVER.md estimates 15 to 30×, which is a
// fair estimate for a smoother trace than this one — the fixture deliberately
// carries sensor wobble and a 300 ms sample period, and both cost entropy. What
// the floor is really for is the failure where a mis-ordered stream or a broken
// delta stops the format compressing at all.
func TestCompressionRatio(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	for _, c := range goldenCases {
		pts := c.points()
		if len(pts) < 2 {
			continue
		}
		blob, err := Encode(pts)
		r.NoError(err)
		js, err := json.Marshal(pts)
		r.NoError(err)

		ratio := float64(len(js)) / float64(len(blob))
		t.Logf("%-14s %5d points  json %7d B  zstd %6d B  %6.1fx  %5.2f B/point",
			c.name, len(pts), len(js), len(blob), ratio, float64(len(blob))/float64(len(pts)))

		if c.name == "lap-300" {
			r.Greater(ratio, 12.0, "the format has stopped compressing a realistic lap")
		}
	}
}

func name(n int) string {
	switch n {
	case MaxPoints:
		return "4096-at-the-cap"
	case 1:
		return "1-point"
	default:
		return "300-points"
	}
}
