package wire

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCodeLiterals pins the bytes, not the constants. A test that compared
// CodeConflict against CodeConflict would pass after someone changed the literal
// to "Conflict", and every deployed client branching on the old spelling would
// stop branching. The strings below are quoted from the error table of
// docs/API-V1.md.
func TestCodeLiterals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code      Code
		literal   string
		status    int
		retryable bool
	}{
		{CodeUnauthorized, "unauthorized", http.StatusUnauthorized, false},
		{CodeForbidden, "forbidden", http.StatusForbidden, false},
		{CodeNotFound, "not_found", http.StatusNotFound, false},
		{CodeConflict, "conflict", http.StatusConflict, false},
		{CodeInvalid, "invalid", http.StatusUnprocessableEntity, false},
		{CodeRateLimited, "rate_limited", http.StatusTooManyRequests, true},
		{CodeServerError, "server_error", http.StatusInternalServerError, true},
		{CodeClientTooOld, "client_too_old", http.StatusUpgradeRequired, false},
	}

	for _, tt := range tests {
		t.Run(tt.literal, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			r.Equal(tt.literal, string(tt.code))
			r.Equal(tt.status, tt.code.HTTPStatus())
			r.Equal(tt.retryable, tt.code.Retryable())
			r.True(tt.code.Known())

			// And through the envelope, which is how it actually travels.
			b, err := json.Marshal(NewError(tt.code, "A sentence for the driver."))
			r.NoError(err)
			r.JSONEq(`{"error":{"code":"`+tt.literal+`","message":"A sentence for the driver."}}`, string(b))
		})
	}

	t.Run("Codes lists exactly the v1 codes", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		got := Codes()
		r.Len(got, len(tests))
		for i, tt := range tests {
			r.Equal(tt.code, got[i], "Codes is in the order of the spec's table")
		}
		r.NotSame(&got, ptrOf(Codes()), "Codes hands out a fresh slice")
	})

	t.Run("an unknown code is not guessed at", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		var c Code = "teapot"
		r.False(c.Known())
		r.Zero(c.HTTPStatus(), "an unknown code has no status; the client uses the HTTP one")
		r.False(c.Retryable())
	})
}

func TestErrorIsAnError(t *testing.T) {
	t.Parallel()

	t.Run("renders for a log, not for a driver", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		err := &Error{Code: CodeInvalid, Message: "The lap could not be read."}
		r.Equal("invalid: The lap could not be read.", err.Error())
	})

	t.Run("survives a trip up a stack", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		var err error = &Error{Code: CodeRateLimited, Message: "Slow down.", RetryAfterS: 12}
		wrapped := errors.Join(errors.New("uploading lap 7"), err)

		var got *Error
		r.ErrorAs(wrapped, &got)
		r.Equal(CodeRateLimited, got.Code)
		r.Equal(12, got.RetryAfterS)
		r.True(got.Code.Retryable())
	})

	t.Run("a nil Error does not panic", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		var err *Error
		r.NotPanics(func() { _ = err.Error() })
	})

	t.Run("an envelope with no error decodes to nil", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		var env ErrorEnvelope
		r.NoError(json.Unmarshal([]byte(`{}`), &env))
		r.Nil(env.Error)
	})
}

// TestFeatureGating pins the two-sided rule: a capability is present only when
// the server declares it and the driver is entitled to it. Getting this backwards
// is how a paid button appears on a free account.
func TestFeatureGating(t *testing.T) {
	t.Parallel()

	d := Discovery{Features: []Feature{FeatureTelemetry, FeatureReference, FeatureLive, FeatureTTS}}
	me := Me{Features: []Feature{FeatureTelemetry, FeatureReference}}

	tests := []struct {
		feature Feature
		server  bool
		driver  bool
	}{
		{FeatureTelemetry, true, true},
		{FeatureReference, true, true},
		{FeatureLive, true, false},
		{FeatureTTS, true, false},
		{FeatureField, false, false},
		{FeatureSetups, false, false},
		{FeatureCoach, false, false},
		{FeatureCompetition, false, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.feature), func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			r.Equal(tt.server, d.Has(tt.feature))
			r.Equal(tt.driver, me.Has(tt.feature))
		})
	}

	t.Run("an empty list grants nothing", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		r.False(Discovery{}.Has(FeatureTelemetry))
		r.False(Me{}.Has(FeatureTelemetry))
	})
}

// TestFeatureLiterals pins the feature names, which travel as bytes in two
// documents and gate every optional part of the client.
func TestFeatureLiterals(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	r.Equal("telemetry", string(FeatureTelemetry))
	r.Equal("reference", string(FeatureReference))
	r.Equal("live", string(FeatureLive))
	r.Equal("field", string(FeatureField))
	r.Equal("tts", string(FeatureTTS))
	r.Equal("setups", string(FeatureSetups))
	r.Equal("coach", string(FeatureCoach))
	r.Equal("competition", string(FeatureCompetition))
}

// TestVocabularyLiterals pins the rest of the closed vocabularies. The lap kinds
// in particular are the client's own recovered wire values.
func TestVocabularyLiterals(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	r.Equal("clean", string(KindClean))
	r.Equal("in", string(KindIn))
	r.Equal("out", string(KindOut))
	r.Equal("invalid", string(KindInvalid))

	r.Equal("practice", string(SessionPractice))
	r.Equal("qualifying", string(SessionQualifying))
	r.Equal("race", string(SessionRace))
	r.Equal("testing", string(SessionTesting))

	r.Equal("self", string(ScopeSelf))
	r.Equal("car", string(ScopeCar))
	r.Equal("class", string(ScopeClass))

	r.Equal("pending", string(StatusPending))
	r.Equal("approved", string(StatusApproved))
	r.Equal("denied", string(StatusDenied))
	r.Equal("expired", string(StatusExpired))

	r.Equal("green", string(FlagGreen))
	r.Equal("yellow", string(FlagYellow))
	r.Equal("red", string(FlagRed))
	r.Equal("white", string(FlagWhite))
	r.Equal("checkered", string(FlagCheckered))
	r.Equal("black", string(FlagBlack))
}

// TestTracePointTagsMatchTheClient pins the twelve keys in the order the capture
// client declares them. It is written as one expected document rather than as a
// set of tags because order is part of what makes the two sides produce
// identical bytes, and a set would not notice it changing.
func TestTracePointTagsMatchTheClient(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	p := TracePoint{
		OffsetMs: 1, SpeedKmh: 2, Throttle: 3, Brake: 4, Gear: 5, RPM: 6,
		Steer: 7, DistPct: 8, LatG: 9, LongG: 10, La: 11, Lo: 12,
	}
	b, err := json.Marshal(p)
	r.NoError(err)
	r.Equal(
		`{"t":1,"v":2,"thr":3,"brk":4,"g":5,"r":6,"st":7,"p":8,"lg":9,"og":10,"la":11,"lo":12}`,
		string(b),
		"the twelve keys, their order and their values are the contract with the capture client",
	)

	var back TracePoint
	r.NoError(json.Unmarshal(b, &back))
	r.Equal(p, back)
}

func TestHasGPS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		p    TracePoint
		want bool
	}{
		{"both zero is absent", TracePoint{}, false},
		{"both set", TracePoint{La: 4157311, Lo: 209044}, true},
		{"latitude only", TracePoint{La: 4157311}, true},
		{"longitude only", TracePoint{Lo: 209044}, true},
		{"negative", TracePoint{La: -3390000, Lo: -7060000}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.New(t).Equal(tt.want, tt.p.HasGPS())
		})
	}
}

// TestLivePointIsFlat pins the promotion: the spec says POST /live carries "Trace
// keys plus lap, on_track, on_pit_road", so an embedded struct that ever gained a
// tag of its own would nest the trace keys and silently change the endpoint.
func TestLivePointIsFlat(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	b, err := json.Marshal(LivePoint{
		TracePoint: TracePoint{OffsetMs: 1, SpeedKmh: 2},
		Lap:        12, OnTrack: true, OnPitRoad: false,
	})
	r.NoError(err)
	r.NotContains(string(b), `"TracePoint"`)
	r.Contains(string(b), `"t":1`)
	r.Contains(string(b), `"lap":12`)
	r.Contains(string(b), `"on_track":true`)
	r.Contains(string(b), `"on_pit_road":false`)
}

// TestOptionalFieldsStayDistinguishable pins the nullable fields the spec calls
// out. A *string that became a string would turn "the server attached no
// competition session" into "the server attached session \"\"".
func TestOptionalFieldsStayDistinguishable(t *testing.T) {
	t.Parallel()

	t.Run("a stint with no session", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		var got StintResult
		r.NoError(json.Unmarshal([]byte(`{"stint_id":"s","session_id":null}`), &got))
		r.Nil(got.SessionID)

		b, err := json.Marshal(got)
		r.NoError(err)
		r.JSONEq(`{"stint_id":"s","session_id":null}`, string(b))
	})

	t.Run("a driver with no team", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		var got Me
		r.NoError(json.Unmarshal([]byte(`{"driver":{"id":"d"},"team":null,"features":[]}`), &got))
		r.Nil(got.Team)
	})

	t.Run("a summary that is not final", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		var got Summary
		r.NoError(json.Unmarshal([]byte(`{"laps":3}`), &got))
		r.Nil(got.FinishedAt, "no finished_at means the stint is still running")

		b, err := json.Marshal(got)
		r.NoError(err)
		r.NotContains(string(b), "finished_at")
	})

	t.Run("a tts request with no voice", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		b, err := json.Marshal(TTSRequest{Text: "Box this lap.", Lang: "en"})
		r.NoError(err)
		r.JSONEq(`{"text":"Box this lap.","voice":null,"lang":"en"}`, string(b))
	})
}

// TestTimesCarryTheirOffset pins the "Conventions" rule that times are RFC 3339
// with an offset. A server that normalised to UTC would still be correct; a
// client that dropped the offset would not be.
func TestTimesCarryTheirOffset(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	b, err := json.Marshal(Stint{StartedAt: at})
	r.NoError(err)
	r.Contains(string(b), `"started_at":"2026-09-12T14:03:11+02:00"`)

	var back Stint
	r.NoError(json.Unmarshal(b, &back))
	r.True(back.StartedAt.Equal(at))
}

func ptrOf[T any](v T) *T { return &v }
