package trace

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pacenote-sim/protocol/wire"
)

// TestRoundTripProperty is the property the whole module exists to guarantee,
// asserted over a few thousand generated traces rather than over a handful of
// hand-written ones: whatever Encode accepts, Decode returns unchanged.
//
// The generator is a linear congruential one written out in vectors_test.go, so
// a failure names a seed that reproduces exactly. It sweeps lengths across the
// interesting boundaries — zero, one, the varint width changes, the cap — and
// it sweeps shapes, because a channel that is constant compresses through a
// different path than one that is noise.
func TestRoundTripProperty(t *testing.T) {
	t.Parallel()

	lengths := []int{0, 1, 2, 3, 7, 15, 16, 63, 64, 127, 128, 129, 255, 256, 300, 1000, 4095, MaxPoints}
	shapes := []struct {
		name string
		make func(seed uint32, n int) []wire.TracePoint
	}{
		{"smooth", func(_ uint32, n int) []wire.TracePoint { return realisticLap(n) }},
		{"constant", func(_ uint32, n int) []wire.TracePoint { return constantRun(n) }},
		{"no gps", func(_ uint32, n int) []wire.TracePoint { return stripGPS(realisticLap(n)) }},
		{"noise", randomTrace},
		{"bounds", boundsTrace},
	}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			dst := make([]wire.TracePoint, 0, MaxPoints)
			for _, n := range lengths {
				for seed := uint32(1); seed <= 8; seed++ {
					pts := shape.make(seed*2654435761, n)
					r.NoError(Validate(pts), "%s/%d/%d: the generator produced an illegal trace", shape.name, n, seed)

					blob, err := Encode(pts)
					r.NoErrorf(err, "%s/%d/%d", shape.name, n, seed)

					v, err := Version(blob)
					r.NoError(err)
					r.Equal(CodecVersion, v)

					dst, err = Decode(blob, dst)
					r.NoErrorf(err, "%s/%d/%d", shape.name, n, seed)
					r.Lenf(dst, len(pts), "%s/%d/%d", shape.name, n, seed)
					for i := range pts {
						r.Equalf(pts[i], dst[i], "%s/%d/%d point %d", shape.name, n, seed, i)
					}

					// Encoding the decoded trace must give the same bytes: the
					// decoder cannot reach a state the encoder cannot express.
					again, err := Encode(dst)
					r.NoError(err)
					r.Equalf(blob, again, "%s/%d/%d is not a fixed point", shape.name, n, seed)
				}
			}
		})
	}
}

// TestEncodedSizeIsSane is a loose guard on the thing the format is for. It is
// not a benchmark and it is not a budget: it fails only if the codec has stopped
// compressing at all, which is what a broken delta or a mis-ordered stream looks
// like from the outside.
func TestEncodedSizeIsSane(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	blob, err := Encode(realisticLap(300))
	r.NoError(err)
	r.Less(len(blob), 4096, "a 300-point lap should be a kilobyte or two, not %d bytes", len(blob))

	flat, err := Encode(constantRun(4096))
	r.NoError(err)
	r.Less(len(flat), 512, "a stationary car should compress to almost nothing, not %d bytes", len(flat))
}

// randomTrace is white noise inside every legal bound: the worst case for delta
// coding and the best test of the varint widths.
func randomTrace(seed uint32, n int) []wire.TracePoint {
	g := newNoise(seed)
	pts := make([]wire.TracePoint, n)
	t := 0
	for i := range pts {
		t += 1 + (g.next(50) + 50)
		pts[i] = wire.TracePoint{
			OffsetMs: min(t, MaxOffsetMs),
			SpeedKmh: g.next(MaxSpeedKmh/2) + MaxSpeedKmh/2,
			Throttle: g.next(MaxPercent/2) + MaxPercent/2,
			Brake:    g.next(MaxPercent/2) + MaxPercent/2,
			Gear:     g.next(5) + 5,
			RPM:      g.next(MaxRPM/2) + MaxRPM/2,
			Steer:    g.next(MaxSteerDeg),
			DistPct:  g.next(MaxDistPct/2) + MaxDistPct/2,
			LatG:     g.next(MaxAccelG),
			LongG:    g.next(MaxAccelG),
			La:       g.next(MaxLat),
			Lo:       g.next(MaxLon),
		}
		if i > 0 && pts[i].OffsetMs <= pts[i-1].OffsetMs {
			pts[i].OffsetMs = pts[i-1].OffsetMs + 1
		}
		t = pts[i].OffsetMs
	}
	return pts
}

// boundsTrace alternates every channel between its two extremes, which is where
// a sign error in the zig-zag mapping shows up as a value rather than as a
// crash.
func boundsTrace(seed uint32, n int) []wire.TracePoint {
	g := newNoise(seed)
	pts := make([]wire.TracePoint, n)
	for i := range pts {
		hi := (i+int(seed))%2 == 0
		pick := func(a, b int) int {
			if hi {
				return a
			}
			return b
		}
		pts[i] = wire.TracePoint{
			OffsetMs: i * (MaxOffsetMs / max(n, 1)),
			SpeedKmh: pick(0, MaxSpeedKmh),
			Throttle: pick(0, MaxPercent),
			Brake:    pick(MaxPercent, 0),
			Gear:     pick(MinGear, MaxGear),
			RPM:      pick(0, MaxRPM),
			Steer:    pick(-MaxSteerDeg, MaxSteerDeg),
			DistPct:  pick(0, MaxDistPct),
			LatG:     pick(-MaxAccelG, MaxAccelG),
			LongG:    pick(MaxAccelG, -MaxAccelG),
			La:       clamp(pick(-MaxLat, MaxLat)+g.next(1), -MaxLat, MaxLat),
			Lo:       clamp(pick(MaxLon, -MaxLon)+g.next(1), -MaxLon, MaxLon),
		}
	}
	return pts
}
