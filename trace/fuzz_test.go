package trace

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pacenote-sim/protocol/wire"
)

const corpusDir = "testdata/fuzz/FuzzDecode"

// corpusSeeds is the committed FuzzDecode corpus: every golden vector, and a set
// of deliberate corruptions of them.
//
// Seeding from the golden vectors is the point. Random bytes almost never form a
// valid header, so a fuzzer started from nothing spends its whole budget in the
// first twenty lines of Decode and never reaches the twelve stream loops, which
// are where the interesting mistakes would be. Starting from a real blob, one
// mutated byte lands in the middle of a varint stream.
func corpusSeeds() (map[string][]byte, error) {
	seeds := map[string][]byte{
		"empty":           {},
		"version-only":    {CodecVersion},
		"bad-version":     {CodecVersion + 1, 0x00},
		"zero-version":    {0x00, 0x00},
		"header-only":     {CodecVersion, 0x01},
		"count-overlong":  append([]byte{CodecVersion}, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff),
		"count-too-many":  countClaim(MaxPoints + 1),
		"count-at-cap":    countClaim(MaxPoints),
		"not-zstd":        {CodecVersion, 0x02, 'n', 'o', 't', 'z', 's', 't', 'd'},
		"empty-plaintext": mustEncode(nil),
	}
	for _, c := range goldenCases {
		blob, err := Encode(c.points())
		if err != nil {
			return nil, fmt.Errorf("seed %s: %w", c.name, err)
		}
		seeds["golden-"+c.name] = blob
		// Truncations: the header alone, half a blob, and one byte short.
		for _, cut := range []int{1, 2, len(blob) / 2, len(blob) - 1} {
			if cut > 0 && cut < len(blob) {
				seeds[fmt.Sprintf("trunc-%s-%d", c.name, cut)] = blob[:cut:cut]
			}
		}
		// Single-byte corruptions spread across the compressed payload.
		for _, at := range []int{2, len(blob) / 3, 2 * len(blob) / 3, len(blob) - 1} {
			if at >= 1 && at < len(blob) {
				bad := append([]byte(nil), blob...)
				bad[at] ^= 0x5a
				seeds[fmt.Sprintf("flip-%s-%d", c.name, at)] = bad
			}
		}
	}
	return seeds, nil
}

func countClaim(n uint64) []byte {
	b := []byte{CodecVersion}
	return binary.AppendUvarint(b, n)
}

func mustEncode(pts []wire.TracePoint) []byte {
	b, err := Encode(pts)
	if err != nil { // unreachable: the callers pass traces Validate accepts
		panic(err)
	}
	return b
}

// FuzzDecode puts arbitrary bytes through the decoder. The committed corpus in
// testdata/fuzz/FuzzDecode is loaded as ordinary test cases by `go test`, so a
// finding stays failing after the fuzzer that found it has stopped running.
//
// Three properties, and the third is the one that earns its keep: it is not
// enough that a hostile blob fails to crash the server, it must also be
// impossible for one to smuggle a trace past Decode that Encode would have
// refused to write. Whatever comes out of Decode is a legal trace or there is no
// trace at all.
func FuzzDecode(f *testing.F) {
	seeds, err := corpusSeeds()
	require.NoError(f, err)
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, b []byte) {
		// Version must answer, or refuse, for any input at all.
		if v, err := Version(b); err == nil {
			require.GreaterOrEqual(t, v, 0)
		} else {
			require.Empty(t, b, "Version only refuses an empty blob")
		}

		dst := make([]wire.TracePoint, 0, 8)
		got, err := Decode(b, dst)
		if err != nil {
			require.Empty(t, got, "a failed Decode returns the buffer, emptied")
			return
		}

		require.LessOrEqual(t, len(got), MaxPoints, "Decode returned more points than the cap")
		require.NoError(t, Validate(got), "Decode returned a trace Encode would have refused")

		// A decoded trace must re-encode and come back identical: the decoder
		// cannot invent a state the encoder has no way to express.
		again, err := Encode(got)
		require.NoError(t, err)
		back, err := Decode(again, nil)
		require.NoError(t, err)
		equalTrace(t, got, back, "re-encoding a decoded trace is not a fixed point")
	})
}

// FuzzRoundTrip drives the other direction: arbitrary bytes shaped into a legal
// trace, encoded, and decoded again. It is the property test with the generator
// replaced by a fuzzer, and it is what would catch a channel whose delta coding
// is wrong only for values the hand-written cases never reach.
func FuzzRoundTrip(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte("a single short seed"))
	for _, c := range goldenCases {
		pts := c.points()
		raw := make([]byte, 0, len(pts)*3)
		for _, p := range pts {
			raw = append(raw, byte(p.SpeedKmh), byte(p.Throttle), byte(p.Gear+1))
		}
		f.Add(raw)
	}

	f.Fuzz(func(t *testing.T, raw []byte) {
		pts := traceFromBytes(raw)
		require.NoError(t, Validate(pts), "the generator must only produce legal traces")

		blob, err := Encode(pts)
		require.NoError(t, err)

		v, err := Version(blob)
		require.NoError(t, err)
		require.Equal(t, CodecVersion, v)

		got, err := Decode(blob, nil)
		require.NoError(t, err)
		equalTrace(t, pts, got, "round trip")
	})
}

// traceFromBytes shapes arbitrary bytes into a legal trace: three bytes per
// sample, each channel folded into its own range and t forced to increase. It
// deliberately produces values the realistic generator never would.
func traceFromBytes(raw []byte) []wire.TracePoint {
	n := min(len(raw)/3, MaxPoints)
	pts := make([]wire.TracePoint, n)
	t := 0
	for i := range pts {
		a, b, c := int(raw[i*3]), int(raw[i*3+1]), int(raw[i*3+2])
		t += 1 + a%97
		if t > MaxOffsetMs {
			pts = pts[:i]
			break
		}
		pts[i] = wire.TracePoint{
			OffsetMs: t,
			SpeedKmh: (a * 3) % (MaxSpeedKmh + 1),
			Throttle: b % (MaxPercent + 1),
			Brake:    c % (MaxPercent + 1),
			Gear:     MinGear + (a+b)%(MaxGear-MinGear+1),
			RPM:      (a * b * 7) % (MaxRPM + 1),
			Steer:    (a*b)%(2*MaxSteerDeg+1) - MaxSteerDeg,
			DistPct:  (b * c) % (MaxDistPct + 1),
			LatG:     (b*c)%(2*MaxAccelG+1) - MaxAccelG,
			LongG:    (a*c)%(2*MaxAccelG+1) - MaxAccelG,
			La:       (a*b*c*911)%(2*MaxLat+1) - MaxLat,
			Lo:       (a*b*c*977)%(2*MaxLon+1) - MaxLon,
		}
	}
	return pts
}
