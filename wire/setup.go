package wire

// The car's setup, as a simulator publishes it.
//
// It rides on [Stint] because it is one per sitting: a driver builds a setup in
// the garage and drives it, and it does not change from lap to lap. A stint is
// idempotent, so a client that learns the setup late simply sends the stint
// again with it attached.
//
// It is optional and its absence is normal. A simulator that publishes no setup
// at all, a series that locks the setup and hides it, and a client too old to
// read it are the same thing on the wire — no setup — and none of them is an
// error.
//
// # Why this shape
//
// A setup sheet is car-specific: a GT3 car has a rear wing and a dive plane
// count, a stock car has neither and has a track bar instead. Two shapes fall
// out of that, and this is both of them.
//
// The normalised core is what every car on a track has: four wheels with a cold
// pressure, a hot pressure, three temperatures across the tread and three tread
// depths, plus the rear wing where the car has one. Those are the measurements
// that answer a setup question without an opinion in them. Tread temperature
// across a tyre is the canonical camber reading — an inside edge twelve degrees
// hotter than the outside is a car leaning on the wrong part of the contact
// patch, and no survey of the driver is needed to establish it — and the hot
// pressure against the cold one says exactly how far the cold setting has to
// move to land where the tyre wants to be.
//
// Everything else is carried as declared key-values: the group it was published
// under, the name the simulator spells it with, what it printed, and the leading
// number and unit where it printed one. That is what lets a car nobody has seen
// arrive intact without a release of this package, and it is still not a dump of
// YAML: every entry is named, and a consumer can look one up rather than parse
// a document.

// Wheel names a corner of the car. The four spellings are frozen wire literals.
type Wheel string

// The four wheels.
const (
	WheelLF Wheel = "lf"
	WheelRF Wheel = "rf"
	WheelLR Wheel = "lr"
	WheelRR Wheel = "rr"
)

// SetupValue is one named setting of a setup sheet.
//
// Text is what the simulator printed and is authoritative: "−2.5 deg", "Soft",
// "9 hole", "0 clicks". Number and Unit are that text read as a measurement
// where it is one — −2.5 and "deg" — and are the leading number and the rest of
// the line, nothing cleverer. A setting whose text carries no number has Number
// zero and Unit empty, which is indistinguishable from a setting whose value
// really is a bare zero; Text is what separates the two, which is why it is
// always present.
type SetupValue struct {
	// Group is the path the simulator published this under, slash-separated
	// ("Chassis/LeftFront"). It is empty for a value published at the top of
	// the block.
	Group string `json:"group,omitempty"`
	// Name is the key, spelled the way the simulator spells it, so that a
	// driver can find the control on the setup screen.
	Name string `json:"name"`
	// Text is the value as published.
	Text string `json:"text"`
	// Number is the leading number of Text, and Unit what followed it.
	Number float64 `json:"number,omitempty"`
	Unit   string  `json:"unit,omitempty"`
}

// SetupTyre is one wheel of a [CarSetup]: what it was set to and what it came
// back at.
//
// The three temperatures and the three tread depths are ordered inner, middle,
// outer as the car stands, whichever order the simulator published them in. A
// simulator that reports a left tyre outside-in and a right tyre inside-out is
// normalised here, once, so that "the inners were hotter than the outers" is
// the same comparison on both sides of the car.
//
// A field left at zero was not published. Nothing here is negative and a real
// tyre is never at zero kPa, zero degrees or zero tread.
type SetupTyre struct {
	// Wheel is which corner of the car this is.
	Wheel Wheel `json:"wheel"`
	// ColdKpa is the starting pressure the setup sheet holds and HotKpa the
	// pressure the tyre was last read at, both in kPa. The difference is how
	// far the cold setting has to move.
	ColdKpa float64 `json:"cold_kpa,omitempty"`
	HotKpa  float64 `json:"hot_kpa,omitempty"`
	// The last tread temperatures across the tyre, in °C, inner to outer.
	TempInnerC  float64 `json:"temp_inner_c,omitempty"`
	TempMiddleC float64 `json:"temp_middle_c,omitempty"`
	TempOuterC  float64 `json:"temp_outer_c,omitempty"`
	// The tread remaining across the tyre, in percent, inner to outer.
	TreadInnerPct  float64 `json:"tread_inner_pct,omitempty"`
	TreadMiddlePct float64 `json:"tread_middle_pct,omitempty"`
	TreadOuterPct  float64 `json:"tread_outer_pct,omitempty"`
}

// CarSetup is the car's setup for a stint.
type CarSetup struct {
	// UpdateCount is the simulator's own revision counter for the sheet, when
	// it publishes one. Two stints of the same sitting with different counts
	// were driven on different setups.
	UpdateCount int `json:"update_count,omitempty"`
	// Tyres is one entry per wheel the simulator published, in the order LF,
	// RF, LR, RR. A car whose tyres are not published at all has none.
	Tyres []SetupTyre `json:"tyres,omitempty"`
	// RearWing is the rear wing setting, for the cars that have one. It is
	// lifted out of Values and does not appear there as well.
	RearWing *SetupValue `json:"rear_wing,omitempty"`
	// Values is every other setting of the sheet — aero, chassis, brakes,
	// drivetrain, fuel — in the order the simulator published them.
	Values []SetupValue `json:"values,omitempty"`
}
