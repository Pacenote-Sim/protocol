package wire

import "time"

// PairStatus is the state of a device-code pairing as POST /pair/poll reports
// it. Only StatusApproved carries a token.
type PairStatus string

// The v1 pairing states.
const (
	StatusPending  PairStatus = "pending"  // keep polling, no faster than IntervalS
	StatusApproved PairStatus = "approved" // Token and Driver are set; stop polling
	StatusDenied   PairStatus = "denied"   // the driver refused; stop polling
	StatusExpired  PairStatus = "expired"  // the device code timed out; start again
)

// SessionType is what the driver was doing, and it is the same vocabulary the
// simulator uses.
type SessionType string

// The v1 session types.
const (
	SessionPractice   SessionType = "practice"
	SessionQualifying SessionType = "qualifying"
	SessionRace       SessionType = "race"
	SessionTesting    SessionType = "testing"
)

// Kind classifies a completed lap. The four values are frozen wire literals: the
// capture client emits exactly these bytes and the server stores them.
type Kind string

// The v1 lap kinds.
const (
	KindClean   Kind = "clean"   // a full lap from the line, on track throughout
	KindIn      Kind = "in"      // entered the pits
	KindOut     Kind = "out"     // started on pit road
	KindInvalid Kind = "invalid" // driven, but not to be counted
)

// Scope says how wide a net a reference lap was drawn from. It is a preference
// on the request and a statement of fact on the response: a server returns the
// narrowest scope it actually has and names it, so a server with no class data
// can still answer with the driver's own best rather than a 404.
type Scope string

// The v1 reference-lap scopes, narrowest first.
const (
	ScopeSelf  Scope = "self"  // this driver's own best
	ScopeCar   Scope = "car"   // the best anyone has driven in this car
	ScopeClass Scope = "class" // the best anyone has driven in this class
)

// PairStart is the answer to POST /pair/start, an unauthenticated standard
// device-code grant. The call has no request body.
//
// DeviceCode is the secret the client polls with and never shows anyone.
// UserCode is the short human code, "H4T-9KQ", that the driver types into
// VerificationURI in a browser. IntervalS is a floor, not a suggestion: a client
// must not poll faster. ExpiresInS is how long the code stays good.
type PairStart struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	IntervalS       int    `json:"interval_s"`
	ExpiresInS      int    `json:"expires_in_s"`
}

// PairPollRequest is the body of POST /pair/poll: the device code and nothing
// else. The call is unauthenticated — a pairing is how a client gets its first
// token, so it cannot present one.
type PairPollRequest struct {
	DeviceCode string `json:"device_code"`
}

// PairPoll is the answer to POST /pair/poll. Token and Driver are present only
// when Status is [StatusApproved]; the token is a bearer credential that the
// client stores and never logs.
type PairPoll struct {
	Status PairStatus `json:"status"`
	Token  string     `json:"token,omitempty"`
	Driver *Driver    `json:"driver,omitempty"`
}

// Driver is the identity behind a token, as the server knows it. Slug is the
// stable URL-safe name, Class is the driver's licence or category ("gt3"), and
// Avatar is an absolute URL or empty.
type Driver struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Class  string `json:"class"`
	Avatar string `json:"avatar,omitempty"`
}

// Team is the driver's team, or nil when they drive alone.
type Team struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Me is the answer to GET /me: who this token belongs to and what it may do.
//
// Features here is the intersection of the server's features with this driver's
// entitlements, so a free account and a paid one get different buttons on the
// same server. Once authenticated the client uses this list and not
// [Discovery.Features].
type Me struct {
	Driver   Driver    `json:"driver"`
	Team     *Team     `json:"team"`
	Features []Feature `json:"features"`
}

// Has reports whether this driver is entitled to the feature. A capability needs
// both this and [Discovery.Has].
func (m Me) Has(f Feature) bool { return hasFeature(m.Features, f) }

// Stint is the body of PUT /stints/{stint_id}: one continuous capture, one car,
// one track, one session type, one sitting. The client generates the id, which
// is a UUIDv7 in the path, so it can create a stint offline and upload it hours
// later and a retry is always safe.
//
// The call is idempotent: repeating it updates the mutable fields and returns
// the same stint. TrackID is the simulator's own stable identifier ("barcelona
// gp") and Track is the display name. Sectors are the sector start positions as
// fractions of the lap, 0…1, ascending, the first conventionally 0.
//
// Setup is the car's setup for this sitting, or nil. It is on the stint and not
// on the lap because it is one per sitting, and it is optional because a
// simulator that publishes no setup, and a series that locks it away, are
// normal rather than broken. See [CarSetup].
type Stint struct {
	Sim         string      `json:"sim"`
	Track       string      `json:"track"`
	TrackID     string      `json:"track_id"`
	Car         string      `json:"car"`
	CarClass    string      `json:"car_class"`
	SessionType SessionType `json:"session_type"`
	StartedAt   time.Time   `json:"started_at"`
	Sectors     []float64   `json:"sectors"`
	Setup       *CarSetup   `json:"setup,omitempty"`
}

// StintResult is the answer to PUT /stints/{stint_id}. SessionID is the server's
// own competition session if it chose to attach one; the client only ever echoes
// it back in the user interface and never sends it anywhere.
type StintResult struct {
	StintID   string  `json:"stint_id"`
	SessionID *string `json:"session_id"`
}

// Lap is one completed lap with its trace. Number is the simulator's lap
// counter, LapMs the lap time in milliseconds, StartedAt when the lap began.
//
// Laps are append-only and idempotent on (stint_id, number): the same number
// with identical content is accepted and counted once, and with different
// content it is [CodeConflict]. A trace longer than [Limits.TracePoints] is
// rejected.
//
// Corners is the client's own corner analysis of this lap, worst first, or
// empty. It is derived from the trace and a reference the client chose, so it
// is not recomputable from the trace alone and is therefore carried rather than
// inferred. See [Corner].
type Lap struct {
	Number    int          `json:"number"`
	LapMs     int          `json:"lap_ms"`
	Kind      Kind         `json:"kind"`
	StartedAt time.Time    `json:"started_at"`
	Trace     []TracePoint `json:"trace"`
	Corners   []Corner     `json:"corners,omitempty"`
}

// LapBatch is the body of POST /stints/{stint_id}/laps: at most
// [Limits.LapsPerRequest] laps, and at most [Limits.MaxBodyBytes] of them.
type LapBatch struct {
	Laps []Lap `json:"laps"`
}

// LapBatchResult is the answer to POST /stints/{stint_id}/laps. Accepted counts
// the laps stored by this call — a repeat of a lap already held counts once, not
// twice. BestLapMs is the best over the whole stint, not over this batch.
type LapBatchResult struct {
	Accepted  int `json:"accepted"`
	BestLapMs int `json:"best_lap_ms"`
}

// Summary is the body of PUT /stints/{stint_id}/summary, sent periodically
// during a stint and once more when it ends. It is a whole-document replace, so
// a retry can never merge two halves of two different states, and a present
// FinishedAt means this one is final.
//
// ConsistencyPct is 0…100. TopSpeedKmh is km/h. BestTrace is the best clean
// lap's trace and obeys [Limits.TracePoints] like any other.
type Summary struct {
	Laps           int          `json:"laps"`
	Incidents      int          `json:"incidents"`
	BestLapMs      int          `json:"best_lap_ms"`
	AvgLapMs       int          `json:"avg_lap_ms"`
	ConsistencyPct int          `json:"consistency_pct"`
	TopSpeedKmh    int          `json:"top_speed_kmh"`
	BestTrace      []TracePoint `json:"best_trace"`
	Conditions     Conditions   `json:"conditions"`
	CarState       CarState     `json:"car_state"`
	FinishedAt     *time.Time   `json:"finished_at,omitempty"`
}

// Conditions are the session-mean weather values of a [Summary].
//
//	Skies        simulator enum, 0 clear … 3 overcast
//	Wetness      simulator enum, 0 unknown … 7 very wet
//	WindKmh      km/h
//	Humidity     percent, 0…100
//	TrackTempC   °C
//	AirTempC     °C
type Conditions struct {
	Skies      int     `json:"skies"`
	Wetness    int     `json:"wetness"`
	WindKmh    float64 `json:"wind_kmh"`
	Humidity   float64 `json:"humidity"`
	TrackTempC float64 `json:"track_temp_c"`
	AirTempC   float64 `json:"air_temp_c"`
}

// CarState is what the car had left at the end of a [Summary]: litres used,
// litres remaining, and the four tyre temperatures in °C.
type CarState struct {
	FuelUsedL  float64   `json:"fuel_used_l"`
	FuelLevelL float64   `json:"fuel_level_l"`
	TyreTempC  TyreTemps `json:"tyre_temp_c"`
}

// TyreTemps is the four corners in °C, left and right, front and rear.
type TyreTemps struct {
	LF float64 `json:"lf"`
	RF float64 `json:"rf"`
	LR float64 `json:"lr"`
	RR float64 `json:"rr"`
}

// ReferenceLap is the answer to GET /reference: the lap the coach's training
// mode compares the driver against, or [CodeNotFound] when the server has none.
//
// Scope is the scope the server actually used, which may be narrower than the
// one asked for. DriverName is a display name and may be empty when the lap is
// the driver's own.
type ReferenceLap struct {
	Scope      Scope        `json:"scope"`
	LapMs      int          `json:"lap_ms"`
	DriverName string       `json:"driver_name,omitempty"`
	Trace      []TracePoint `json:"trace"`
}

// OK is the answer to a call that has nothing to report but success, such as
// PUT /stints/{stint_id}/summary.
type OK struct {
	OK bool `json:"ok"`
}
