package wire

// The corner analysis a client sends with a completed lap.
//
// It rides on [Lap] rather than on [Stint] because it is a property of one lap:
// the same driver in the same car loses a different corner on every lap of a
// session, and that is the whole of what a training cue has to talk about.
//
// It is derived, not raw. A [TracePoint] is a sample and the corner detector's
// output is an answer about a piece of track — where the apex was, how much
// speed it cost against a reference, and what the driver's own pedals were
// doing there. A server storing this stores a dozen numbers per lap beside the
// three hundred samples it already has, and a plugin reading it never has to do
// the arithmetic that produced it.

// CornerPattern names the shape of a mistake, so that a consumer can branch on
// it and a prompt can be written against a fixed vocabulary rather than against
// free text.
//
// The vocabulary is deliberately short: it holds the patterns a detector can
// measure from the throttle and brake channels of one corner, and nothing a
// detector would have to guess at. An empty pattern is the normal case — a
// corner that was slower with nothing conclusive about why — and a consumer
// must treat it as an answer rather than as missing data.
type CornerPattern string

// The v1 corner patterns.
const (
	// PatternEarlyApex is a corner still being slowed at its apex that the
	// driver then has to wait to get back on the power in: the car was turned
	// in too early and the apex arrived before the corner did.
	PatternEarlyApex CornerPattern = "early_apex"
	// PatternLateBraking is a corner whose apex still carries brake pressure,
	// with the throttle picked up normally afterwards.
	PatternLateBraking CornerPattern = "late_braking"
	// PatternSlowExit is a corner the driver is off the brakes in but waits a
	// long way past the apex before picking the throttle up.
	PatternSlowExit CornerPattern = "slow_exit"
)

// Corner is one corner of a completed lap and what it cost.
//
// Turn is the detector's own numbering and not the circuit's. The corners of a
// lap are numbered from 1 in the order they are driven, counting only the ones
// the detector found: a turn taken flat, one whose speed drop is below the
// detector's threshold and one whose apex is below walking pace are not in the
// list, and every later corner shifts down by one when they are dropped. It is
// what the client's own spoken cues have always said out loud ("Turn 4"), so it
// is what a written cue must say too, and a consumer that maps it onto a
// circuit's published turn table is inventing a number this server cannot
// check.
//
// Every speed is whole km/h, because the trace channel it is read from is. The
// two lap positions are per mille of the lap, the same scaling [TracePoint]
// uses for DistPct, so a consumer never has to reconcile two units for one
// axis.
type Corner struct {
	// Turn is the corner's number on this lap, from 1, in the order driven.
	Turn int `json:"turn"`
	// ApexPct is where the apex is, in ‰ of the lap (0…1000).
	ApexPct int `json:"apex_pct"`
	// ApexKmh is the speed at the apex and RefApexKmh the speed the reference
	// lap carried at the same point. A RefApexKmh of zero means the client
	// compared against nothing there.
	ApexKmh    int `json:"apex_kmh"`
	RefApexKmh int `json:"ref_apex_kmh,omitempty"`
	// DeficitKmh is how much apex speed this corner lost against the
	// reference, in whole km/h. It is positive: a client sends the corners
	// that cost time and not the ones that gained it.
	//
	// It is a speed and not a time, because a speed is what is measured. A
	// per-corner time loss would be an integration over a piece of track that
	// the two laps did not necessarily cover at the same points, and calling
	// the result milliseconds would dress an estimate up as a measurement.
	DeficitKmh int `json:"deficit_kmh"`
	// BrakeAtApex is the brake still applied at the apex, in percent. A high
	// value is the driver trail-braking past the apex.
	BrakeAtApex int `json:"brake_at_apex,omitempty"`
	// ThrottleLag is the distance between the apex and the throttle pickup, in
	// ‰ of the lap. A high value is the car not rotated and the driver waiting.
	ThrottleLag int `json:"throttle_lag,omitempty"`
	// Pattern is the shape of the mistake, when the two channels above name
	// one. Empty is normal and means nothing conclusive.
	Pattern CornerPattern `json:"pattern,omitempty"`
}
