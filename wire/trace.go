package wire

// TracePoint is one sample of a lap trace: every channel as a scaled integer so
// that a 300-point lap is a few kilobytes of JSON rather than tens, and a few
// hundred bytes once package trace has compressed it.
//
// The JSON tags and the scaling are frozen. They are the "Trace format" table of
// API-V1.md:
//
//	key   field     meaning                     unit                  range
//	t     OffsetMs  offset from lap start       ms                    ≥ 0
//	p     DistPct   lap distance                ‰ of lap              0…1000
//	v     SpeedKmh  speed                       km/h                  0…700
//	thr   Throttle  throttle                    %                     0…100
//	brk   Brake     brake                       %                     0…100
//	g     Gear      gear                        −1 reverse, 0 neutral −1…10
//	r     RPM       engine speed                rpm                   ≥ 0
//	st    Steer     steering, + is left         degrees               −1000…1000
//	lg    LatG      lateral acceleration        g × 100               −1000…1000
//	og    LongG     longitudinal acceleration   g × 100               −1000…1000
//	la    La        latitude                    degrees × 10⁵         −9000000…9000000
//	lo    Lo        longitude                   degrees × 10⁵         −18000000…18000000
//
// Every key is present on every sample; a producer clamps, it never wraps, and
// it never emits a non-finite value. Within one trace t is strictly increasing.
// la and lo are optional as a pair: both zero means the sample carries no GPS
// fix, which is why they are not pointers — zero is the sentinel the spec chose.
//
// The two open bounds of the spec's table, t and r, and the unstated bounds of
// la and lo, are closed by the codec at trace.MaxOffsetMs, trace.MaxRPM and the
// latitude and longitude of a real planet. They are wide enough that no honest
// sample reaches them and narrow enough that the delta between two of them
// cannot overflow.
//
// The field order below is the capture client's, not the spec table's, and that
// is deliberate: encoding/json writes object keys in struct-declaration order,
// so declaring the fields as telemetry.TracePoint declares them makes a
// marshalled point byte-identical on both sides of the wire rather than merely
// equivalent. The tags are what the contract pins; the order is what makes the
// golden vectors reusable by the client unchanged.
type TracePoint struct {
	OffsetMs int `json:"t"`
	SpeedKmh int `json:"v"`
	Throttle int `json:"thr"`
	Brake    int `json:"brk"`
	Gear     int `json:"g"`
	RPM      int `json:"r"`
	Steer    int `json:"st"`
	DistPct  int `json:"p"`
	LatG     int `json:"lg"`
	LongG    int `json:"og"`
	La       int `json:"la"`
	Lo       int `json:"lo"`
}

// HasGPS reports whether the sample carries a GPS fix. La and Lo are optional as
// a pair and both zero means absent, so a trace recorded without GPS is a valid
// trace and not an error.
func (p TracePoint) HasGPS() bool { return p.La != 0 || p.Lo != 0 }
