package wire

import "time"

// Flag is the session flag a [FieldReport] carries.
type Flag string

// The v1 session flags.
const (
	FlagGreen     Flag = "green"
	FlagYellow    Flag = "yellow"
	FlagRed       Flag = "red"
	FlagWhite     Flag = "white"
	FlagCheckered Flag = "checkered"
	FlagBlack     Flag = "black"
)

// LivePoint is one live sample: the twelve trace keys of a [TracePoint] plus the
// three things that only make sense live. The trace keys are promoted, so a
// LivePoint marshals as a flat object with fifteen keys and not as a nested one.
//
// Lap is the simulator's lap counter. OnTrack is false while the driver is in
// the garage or spectating; OnPitRoad is true on the pit lane.
type LivePoint struct {
	TracePoint
	Lap       int  `json:"lap"`
	OnTrack   bool `json:"on_track"`
	OnPitRoad bool `json:"on_pit_road"`
}

// LiveSample is the body of POST /live, the fire-and-forget sample stream of the
// [FeatureLive] feature.
//
// It is never retried and never queued: a stale live sample is worthless, so a
// failure is dropped silently rather than kept. CurLap is the lap so far, which
// lets a viewer that has just connected draw the trace behind the car instead of
// waiting for the next lap. Sent no more often than [Limits.LiveIntervalMs].
type LiveSample struct {
	StintID string       `json:"stint_id"`
	At      time.Time    `json:"at"`
	Sample  LivePoint    `json:"sample"`
	CurLap  []TracePoint `json:"cur_lap"`
}

// FieldCar is one car of the session field. The tags match the capture client's
// own telemetry.FieldCar byte for byte.
//
//	Num       car number as shown, a string because "07" is not 7
//	Class     car class ("gt3")
//	Lap       the car's lap counter
//	Dist      lap distance as a fraction, 0…1 — note that this is not the ‰ of
//	          a TracePoint: the field relay is not a trace
//	LastMs    last lap time, ms
//	BestMs    best lap time, ms
//	GapMs     gap to the leader, ms
//	Pit       true while the car is on pit road
//	Inc       incident count
//	IsPlayer  true for the car of the client sending the report
type FieldCar struct {
	Num      string  `json:"num"`
	Driver   string  `json:"driver"`
	Class    string  `json:"class"`
	Lap      int     `json:"lap"`
	DistPct  float64 `json:"dist"`
	LastMs   int     `json:"last_ms"`
	BestMs   int     `json:"best_ms"`
	GapMs    int     `json:"gap_ms"`
	Pit      bool    `json:"pit"`
	Inc      int     `json:"inc"`
	IsPlayer bool    `json:"is_player"`
}

// FieldReport is the body of POST /field, the whole-field relay of the
// [FeatureField] feature.
//
// Only one client per session needs to send this and the server decides which:
// see [FieldResult.Armed]. LapsTotal is zero in a timed session. Sent no more
// often than [Limits.FieldIntervalMs].
type FieldReport struct {
	StintID     string      `json:"stint_id"`
	SessionType SessionType `json:"session_type"`
	Flag        Flag        `json:"flag"`
	LapsTotal   int         `json:"laps_total"`
	Cars        []FieldCar  `json:"cars"`
}

// FieldResult is the answer to POST /field. A client that receives Armed false
// is not the authoritative relay for this session and stops sending until its
// next stint; it is not an error and the driver is never told.
type FieldResult struct {
	OK    bool `json:"ok"`
	Armed bool `json:"armed"`
}

// TTSRequest is the body of POST /tts, the spoken-coaching feature. The response
// is audio bytes — audio/wav or audio/mpeg — and not JSON.
//
// Voice is nil for the server's default voice. Lang is a BCP 47 tag ("en").
// The client caches by text, because the same cue is spoken many times a lap,
// and must respect [CodeRateLimited].
type TTSRequest struct {
	Text  string  `json:"text"`
	Voice *string `json:"voice"`
	Lang  string  `json:"lang"`
}
