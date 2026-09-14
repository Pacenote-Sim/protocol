package trace

import (
	"errors"
	"fmt"

	"github.com/pacenote-sim/protocol/wire"
)

// MaxPoints is the hard ceiling on the number of samples in one encoded trace.
// It is not the same thing as the server's advertised Limits.TracePoints, which
// is a policy a deployment chooses and which is typically 300: this is the
// structural bound of the codec itself, the number past which a blob is refused
// before it is decompressed. It exists so that an attacker cannot hand the
// server a two-byte header that turns into gigabytes of samples.
const MaxPoints = 4096

// The bounds of every channel, from the "Trace format" table of API-V1.md. A
// value outside them is refused by [Validate], on the way in and on the way out.
//
// Four of these the specification leaves open, and closing them is this
// package's decision, recorded here because it is a decision and not a reading:
//
//   - t is documented only as "≥ 0". [MaxOffsetMs] is one hour, far longer than
//     any lap of any circuit, and short enough that the difference between two
//     offsets cannot overflow.
//   - r is documented only as "≥ 0". [MaxRPM] is 30000, above any racing engine
//     and any plausible sensor glitch.
//   - la and lo are given no range at all. They are degrees × 10⁵, so the range
//     is the range of latitude and longitude on Earth.
//
// All four are wide enough that no honest sample reaches them, which is the test
// a bound like this has to pass.
const (
	MaxOffsetMs = 3_600_000 // t, ms: one hour
	MaxDistPct  = 1_000     // p, ‰ of the lap
	MaxSpeedKmh = 700       // v, km/h
	MaxPercent  = 100       // thr and brk, %
	MinGear     = -1        // g, reverse
	MaxGear     = 10        // g
	MaxRPM      = 30_000    // r, rpm
	MaxSteerDeg = 1_000     // st, degrees, symmetric
	MaxAccelG   = 1_000     // lg and og, g × 100, symmetric
	MaxLat      = 9_000_000 // la, degrees × 10⁵, symmetric
	MaxLon      = 18_000_000
)

// ErrInvalidTrace reports a trace that breaks the rules of the wire format: a
// channel outside its documented range, or an offset that does not increase.
// [Encode] refuses to write one and [Decode] refuses to return one, so the rule
// holds in both directions and cannot be smuggled past either side.
var ErrInvalidTrace = errors.New("trace: invalid trace")

// ErrTooManyPoints reports a trace, or a blob claiming to hold a trace, longer
// than [MaxPoints].
var ErrTooManyPoints = errors.New("trace: too many points")

// Validate reports whether pts may be encoded, and is also applied to everything
// [Decode] produces.
//
// The rules are the ones API-V1.md states: at most [MaxPoints] samples, every
// channel inside its documented range, and t strictly increasing. A trace with
// no GPS fix is valid — la and lo are optional as a pair and both zero means
// absent — so there is no rule about them beyond their range.
//
// It allocates only when it has something to complain about.
func Validate(pts []wire.TracePoint) error {
	if len(pts) > MaxPoints {
		return fmt.Errorf("%w: %d points, maximum %d", ErrTooManyPoints, len(pts), MaxPoints)
	}
	prevT := -1
	for i := range pts {
		p := &pts[i]
		switch {
		case p.OffsetMs < 0 || p.OffsetMs > MaxOffsetMs:
			return outOfRange(i, "t", p.OffsetMs, 0, MaxOffsetMs)
		case p.SpeedKmh < 0 || p.SpeedKmh > MaxSpeedKmh:
			return outOfRange(i, "v", p.SpeedKmh, 0, MaxSpeedKmh)
		case p.Throttle < 0 || p.Throttle > MaxPercent:
			return outOfRange(i, "thr", p.Throttle, 0, MaxPercent)
		case p.Brake < 0 || p.Brake > MaxPercent:
			return outOfRange(i, "brk", p.Brake, 0, MaxPercent)
		case p.Gear < MinGear || p.Gear > MaxGear:
			return outOfRange(i, "g", p.Gear, MinGear, MaxGear)
		case p.RPM < 0 || p.RPM > MaxRPM:
			return outOfRange(i, "r", p.RPM, 0, MaxRPM)
		case p.Steer < -MaxSteerDeg || p.Steer > MaxSteerDeg:
			return outOfRange(i, "st", p.Steer, -MaxSteerDeg, MaxSteerDeg)
		case p.DistPct < 0 || p.DistPct > MaxDistPct:
			return outOfRange(i, "p", p.DistPct, 0, MaxDistPct)
		case p.LatG < -MaxAccelG || p.LatG > MaxAccelG:
			return outOfRange(i, "lg", p.LatG, -MaxAccelG, MaxAccelG)
		case p.LongG < -MaxAccelG || p.LongG > MaxAccelG:
			return outOfRange(i, "og", p.LongG, -MaxAccelG, MaxAccelG)
		case p.La < -MaxLat || p.La > MaxLat:
			return outOfRange(i, "la", p.La, -MaxLat, MaxLat)
		case p.Lo < -MaxLon || p.Lo > MaxLon:
			return outOfRange(i, "lo", p.Lo, -MaxLon, MaxLon)
		case p.OffsetMs <= prevT:
			return fmt.Errorf("%w: point %d: t %d does not increase on the previous %d",
				ErrInvalidTrace, i, p.OffsetMs, prevT)
		}
		prevT = p.OffsetMs
	}
	return nil
}

func outOfRange(i int, key string, got, lo, hi int) error {
	return fmt.Errorf("%w: point %d: %s is %d, outside %d…%d", ErrInvalidTrace, i, key, got, lo, hi)
}
