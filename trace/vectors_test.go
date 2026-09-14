package trace

import (
	"math"

	"github.com/pacenote-sim/protocol/wire"
)

// The golden vectors, and the deterministic generators behind them. Every
// generator is a pure function of its point count: the committed testdata is
// reproducible from this file alone, which is what makes regenerating it a
// reviewable act rather than a mystery.
//
// Adding a case here and running `go test ./trace -update` writes the pair of
// files. Changing an existing one is a wire-format change; testdata/README.md
// says what that costs.
type goldenCase struct {
	name   string
	why    string
	points func() []wire.TracePoint
}

// goldenCases is the required coverage of the format, and the golden test fails
// if any of these names is missing from testdata/golden.
var goldenCases = []goldenCase{
	{
		name:   "empty",
		why:    "a lap with no samples still has to produce a blob a decoder accepts",
		points: func() []wire.TracePoint { return []wire.TracePoint{} },
	},
	{
		name:   "single",
		why:    "one point: every stream is one delta against zero and nothing else",
		points: func() []wire.TracePoint { return realisticLap(1) },
	},
	{
		name:   "lap-300",
		why:    "the shape the budgets are quoted against: a typical lap at the usual limit",
		points: func() []wire.TracePoint { return realisticLap(300) },
	},
	{
		name:   "lap-4096-cap",
		why:    "exactly MaxPoints: the largest trace that is legal at all",
		points: func() []wire.TracePoint { return realisticLap(MaxPoints) },
	},
	{
		name:   "no-gps",
		why:    "la and lo are optional as a pair and both zero means absent, not invalid",
		points: func() []wire.TracePoint { return stripGPS(realisticLap(300)) },
	},
	{
		name:   "extremes",
		why:    "every channel at both ends of its documented range, alternating, which is also the worst case for delta coding",
		points: extremeTrace,
	},
	{
		name:   "constant-run",
		why:    "a car sitting still: every stream a long run of zero deltas",
		points: func() []wire.TracePoint { return constantRun(512) },
	},
}

// realisticLap builds a plausible 90-second lap of a 4.6 km circuit: five
// braking zones, gears that follow the speed, engine speed that follows the
// gear, and a GPS track that goes round. The channels are smooth because real
// ones are, and they carry a little jitter because real ones do that too — a
// perfectly smooth trace would flatter the codec and the ratio it reports.
func realisticLap(n int) []wire.TracePoint {
	pts := make([]wire.TracePoint, n)
	const (
		lapMs   = 90_000
		corners = 5
	)
	g := newNoise(0x5EED)
	// Sensor and driver wobble, as a bounded random walk rather than as white
	// noise. That distinction is the difference between a fixture that measures
	// the codec and one that measures a random number generator: a real channel
	// is a smooth quantity sampled and truncated, so its sample-to-sample
	// difference is small and correlated, and delta coding is worth something.
	wv := &wobble{g: g, amp: 2}
	wt := &wobble{g: g, amp: 2}
	wr := &wobble{g: g, amp: 25}
	ws := &wobble{g: g, amp: 3}
	wlg := &wobble{g: g, amp: 4}
	wog := &wobble{g: g, amp: 4}
	wla := &wobble{g: g, amp: 2}
	wlo := &wobble{g: g, amp: 2}
	for i := range pts {
		f := 0.0
		if n > 1 {
			f = float64(i) / float64(n-1)
		}
		// Five braking zones as a raised cosine over the lap.
		phase := f * corners * 2 * math.Pi
		corner := 0.5 - 0.5*math.Cos(phase) // 0 on the straight, 1 at the apex
		speed := 285 - 190*corner
		brake := 0.0
		thr := 100.0
		if d := math.Sin(phase - 0.35); d < -0.55 {
			brake = math.Min(100, (-d-0.55)*420)
			thr = 0
		} else if corner > 0.45 {
			thr = math.Max(0, 100-corner*95)
		}
		gear := gearFor(speed)
		rpm := 4200 + 320*speed/float64(gear+1)
		steer := 240 * corner * math.Sin(phase*0.5+1.1)
		latG := 260 * corner * math.Sin(phase*0.5+1.1)
		longG := -300*brake/100 + 120*(1-corner)

		pts[i] = wire.TracePoint{
			OffsetMs: i * lapMs / max(n, 1),
			SpeedKmh: clamp(int(speed)+wv.next(), 0, MaxSpeedKmh),
			Throttle: clamp(int(thr)+wt.next(), 0, MaxPercent),
			Brake:    clamp(int(brake), 0, MaxPercent),
			Gear:     gear,
			RPM:      clamp(int(rpm)+wr.next(), 0, MaxRPM),
			Steer:    clamp(int(steer)+ws.next(), -MaxSteerDeg, MaxSteerDeg),
			DistPct:  clamp(int(f*1000), 0, MaxDistPct),
			LatG:     clamp(int(latG)+wlg.next(), -MaxAccelG, MaxAccelG),
			LongG:    clamp(int(longG)+wog.next(), -MaxAccelG, MaxAccelG),
			// Circuit de Barcelona-Catalunya, degrees × 10⁵, a 1.2 km ellipse.
			La: 4157000 + int(900*math.Sin(f*2*math.Pi)) + wla.next(),
			Lo: 209000 + int(1400*math.Cos(f*2*math.Pi)) + wlo.next(),
		}
	}
	return pts
}

// stripGPS returns the trace with no GPS fix, which the spec spells as both
// channels zero rather than as an absent key.
func stripGPS(pts []wire.TracePoint) []wire.TracePoint {
	for i := range pts {
		pts[i].La = 0
		pts[i].Lo = 0
	}
	return pts
}

// extremeTrace puts every channel on both of its bounds in turn. It is legal by
// [Validate] and pathological for the codec: every delta is the full width of
// its range, so it is the case that proves the varints are sized from the data
// and not from an assumption about it.
func extremeTrace() []wire.TracePoint {
	const n = 16
	pts := make([]wire.TracePoint, n)
	for i := range pts {
		lo := i%2 == 0
		pick := func(a, b int) int {
			if lo {
				return a
			}
			return b
		}
		pts[i] = wire.TracePoint{
			OffsetMs: i * (MaxOffsetMs / (n - 1)),
			SpeedKmh: pick(0, MaxSpeedKmh),
			Throttle: pick(0, MaxPercent),
			Brake:    pick(MaxPercent, 0),
			Gear:     pick(MinGear, MaxGear),
			RPM:      pick(0, MaxRPM),
			Steer:    pick(-MaxSteerDeg, MaxSteerDeg),
			DistPct:  pick(0, MaxDistPct),
			LatG:     pick(-MaxAccelG, MaxAccelG),
			LongG:    pick(MaxAccelG, -MaxAccelG),
			La:       pick(-MaxLat, MaxLat),
			Lo:       pick(MaxLon, -MaxLon),
		}
	}
	return pts
}

// constantRun is a car stopped on pit road with the engine idling: t moves and
// nothing else does, so eleven of the twelve streams are a run of zeroes.
func constantRun(n int) []wire.TracePoint {
	pts := make([]wire.TracePoint, n)
	for i := range pts {
		pts[i] = wire.TracePoint{
			OffsetMs: i * 100,
			SpeedKmh: 0,
			Throttle: 0,
			Brake:    0,
			Gear:     0,
			RPM:      1150,
			Steer:    0,
			DistPct:  742,
			LatG:     0,
			LongG:    0,
			La:       4157311,
			Lo:       209044,
		}
	}
	return pts
}

func gearFor(speed float64) int {
	switch {
	case speed < 70:
		return 1
	case speed < 110:
		return 2
	case speed < 155:
		return 3
	case speed < 200:
		return 4
	case speed < 250:
		return 5
	default:
		return 6
	}
}

// noise is a 32-bit linear congruential generator, written out here rather than
// taken from math/rand so that the committed vectors do not depend on the
// standard library's choice of algorithm.
type noise struct{ state uint32 }

func newNoise(seed uint32) *noise { return &noise{state: seed} }

// next returns a value in −amp…+amp.
func (n *noise) next(amp int) int {
	n.state = n.state*1664525 + 1013904223
	v := int(n.state>>16) % (2*amp + 1)
	return v - amp
}

// wobble is a bounded random walk: one step of −1, 0 or +1 per sample, never
// straying further than amp from the signal it is added to.
type wobble struct {
	g   *noise
	v   int
	amp int
}

func (w *wobble) next() int {
	w.v = clamp(w.v+w.g.next(1), -w.amp, w.amp)
	return w.v
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
