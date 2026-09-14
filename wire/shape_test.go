package wire

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite testdata/shapes.json")

const shapesPath = "testdata/shapes.json"

// at is the one instant every timestamp in the shapes uses: RFC 3339 with an
// offset, which is what the spec's "Conventions" section requires and what a
// naive UTC-only implementation would get wrong.
var at = time.Date(2026, 9, 12, 14, 3, 11, 0, time.FixedZone("CEST", 2*60*60))

func ptr[T any](v T) *T { return &v }

// shape is one pinned type: a value with every field set to something
// distinctive, and a pointer to the zero value of the same type.
//
// Both are needed and they catch different mistakes. The full value catches a
// renamed or swapped tag — every field holds a different value, so two fields
// trading tags changes the document. The zero value catches an omitempty
// appearing or disappearing, which is invisible in a full value and changes what
// a server sees on the wire.
type shape struct {
	name string
	full any
	zero any // a pointer, so the round-trip test has somewhere to decode into
}

func shapes() []shape {
	return []shape{
		{"CarState", CarState{
			FuelUsedL:  48.25,
			FuelLevelL: 12.5,
			TyreTempC:  TyreTemps{LF: 82.1, RF: 84, LR: 79.625, RR: 81.25},
		}, new(CarState)},
		{"CarSetup", CarSetup{
			UpdateCount: 3,
			Tyres: []SetupTyre{{
				Wheel: WheelLF, ColdKpa: 165, HotKpa: 172.5,
				TempInnerC: 92.5, TempMiddleC: 86, TempOuterC: 80.25,
				TreadInnerPct: 97.5, TreadMiddlePct: 98, TreadOuterPct: 98.5,
			}},
			RearWing: &SetupValue{
				Group: "TiresAero/AeroSettings", Name: "RearWingSetting",
				Text: "9 hole", Number: 9, Unit: "hole",
			},
			Values: []SetupValue{{
				Group: "Chassis/LeftFront", Name: "Camber",
				Text: "-2.5 deg", Number: -2.5, Unit: "deg",
			}},
		}, new(CarSetup)},
		{"Conditions", Conditions{
			Skies: 1, Wetness: 2, WindKmh: 8.5, Humidity: 41.25,
			TrackTempC: 34.5, AirTempC: 24.875,
		}, new(Conditions)},
		{"Corner", Corner{
			Turn: 4, ApexPct: 312, ApexKmh: 96, RefApexKmh: 104, DeficitKmh: 8,
			BrakeAtApex: 31, ThrottleLag: 14, Pattern: PatternEarlyApex,
		}, new(Corner)},
		{"Discovery", Discovery{
			API:       "/api/v1",
			Name:      "Iberian GT Championship",
			ShortName: "IGTC",
			Logo:      "https://igtc.example/logo.png",
			Accent:    "#C6F24B",
			Features:  []Feature{FeatureTelemetry, FeatureReference, FeatureLive},
			Limits: Limits{
				TracePoints: 300, LapsPerRequest: 50, LiveIntervalMs: 1000,
				FieldIntervalMs: 2000, SummaryIntervalMs: 30000, MaxBodyBytes: 2097152,
			},
			MinClient:  "1.0.0",
			PairURI:    "https://igtc.example/pair",
			PrivacyURI: "https://igtc.example/privacy",
		}, new(Discovery)},
		{"Driver", Driver{
			ID: "01997f3a-0000-7000-8000-000000000001", Name: "Ana Ruiz",
			Slug: "ana-ruiz", Class: "gt3", Avatar: "https://igtc.example/a.png",
		}, new(Driver)},
		{"Error", Error{
			Code:    CodeConflict,
			Message: "This session no longer accepts laps.",
			Detail:  map[string]any{"field": "lap_ms", "session_id": "41"},
			// 0 would be omitted; the shape has to prove the key exists.
			RetryAfterS: 30,
		}, new(Error)},
		{"ErrorEnvelope", ErrorEnvelope{Error: &Error{
			Code: CodeRateLimited, Message: "Too many uploads. Try again shortly.",
			RetryAfterS: 12,
		}}, new(ErrorEnvelope)},
		{"FieldCar", FieldCar{
			Num: "17", Driver: "M. Costa", Class: "gt3", Lap: 12, DistPct: 0.42,
			LastMs: 91020, BestMs: 90440, GapMs: 3120, Pit: true, Inc: 4, IsPlayer: true,
		}, new(FieldCar)},
		{"FieldReport", FieldReport{
			StintID: "01997f3a-0000-7000-8000-000000000002", SessionType: SessionRace,
			Flag: FlagGreen, LapsTotal: 32,
			Cars: []FieldCar{{Num: "17", Driver: "M. Costa", Class: "gt3", Lap: 12}},
		}, new(FieldReport)},
		{"FieldResult", FieldResult{OK: true, Armed: true}, new(FieldResult)},
		{"Lap", Lap{
			Number: 7, LapMs: 91234, Kind: KindClean, StartedAt: at,
			Trace: []TracePoint{{OffsetMs: 0, SpeedKmh: 211, Throttle: 100, Gear: 5, RPM: 7400, DistPct: 3}},
			Corners: []Corner{{
				Turn: 4, ApexPct: 312, ApexKmh: 96, RefApexKmh: 104, DeficitKmh: 8,
				BrakeAtApex: 31, ThrottleLag: 14, Pattern: PatternEarlyApex,
			}},
		}, new(Lap)},
		{"LapBatch", LapBatch{Laps: []Lap{{Number: 7, LapMs: 91234, Kind: KindOut, StartedAt: at}}}, new(LapBatch)},
		{"LapBatchResult", LapBatchResult{Accepted: 3, BestLapMs: 90118}, new(LapBatchResult)},
		{"Limits", Limits{
			TracePoints: 300, LapsPerRequest: 50, LiveIntervalMs: 1000,
			FieldIntervalMs: 2000, SummaryIntervalMs: 30000, MaxBodyBytes: 2097152,
		}, new(Limits)},
		{"LivePoint", LivePoint{
			TracePoint: TracePoint{
				OffsetMs: 12300, SpeedKmh: 211, Throttle: 98, Brake: 1, Gear: 5,
				RPM: 7400, Steer: -37, DistPct: 421, LatG: -152, LongG: 88,
				La: 4157311, Lo: 209044,
			},
			Lap: 12, OnTrack: true, OnPitRoad: true,
		}, new(LivePoint)},
		{"LiveSample", LiveSample{
			StintID: "01997f3a-0000-7000-8000-000000000003", At: at,
			Sample: LivePoint{TracePoint: TracePoint{OffsetMs: 12300, SpeedKmh: 211}, Lap: 12, OnTrack: true},
			CurLap: []TracePoint{{OffsetMs: 0, SpeedKmh: 205}},
		}, new(LiveSample)},
		{"Me", Me{
			Driver:   Driver{ID: "01997f3a-0000-7000-8000-000000000001", Name: "Ana Ruiz", Slug: "ana-ruiz", Class: "gt3"},
			Team:     &Team{ID: "01997f3a-0000-7000-8000-000000000009", Name: "Escudería Norte"},
			Features: []Feature{FeatureTelemetry, FeatureReference, FeatureTTS},
		}, new(Me)},
		{"OK", OK{OK: true}, new(OK)},
		{"PairPoll", PairPoll{
			Status: StatusApproved, Token: "not-a-real-token",
			Driver: &Driver{ID: "01997f3a-0000-7000-8000-000000000001", Name: "Ana Ruiz", Slug: "ana-ruiz", Class: "gt3"},
		}, new(PairPoll)},
		{"PairPollRequest", PairPollRequest{DeviceCode: "8e1f-not-a-real-device-code"}, new(PairPollRequest)},
		{"PairStart", PairStart{
			DeviceCode: "8e1f-not-a-real-device-code", UserCode: "H4T-9KQ",
			VerificationURI: "https://igtc.example/pair", IntervalS: 2, ExpiresInS: 600,
		}, new(PairStart)},
		{"SetupTyre", SetupTyre{
			Wheel: WheelRR, ColdKpa: 158.5, HotKpa: 169.75,
			TempInnerC: 88.5, TempMiddleC: 84.25, TempOuterC: 79,
			TreadInnerPct: 96.5, TreadMiddlePct: 97.25, TreadOuterPct: 98,
		}, new(SetupTyre)},
		{"SetupValue", SetupValue{
			Group: "Chassis/Front", Name: "ToeIn", Text: "-2.3 mm", Number: -2.3, Unit: "mm",
		}, new(SetupValue)},
		{"ReferenceLap", ReferenceLap{
			Scope: ScopeClass, LapMs: 89440, DriverName: "M. Costa",
			Trace: []TracePoint{{OffsetMs: 0, SpeedKmh: 218}},
		}, new(ReferenceLap)},
		{"Stint", Stint{
			Sim: "iracing", Track: "Circuit de Barcelona-Catalunya", TrackID: "barcelona gp",
			Car: "Ferrari 296 GT3", CarClass: "gt3", SessionType: SessionPractice,
			StartedAt: at, Sectors: []float64{0, 0.31, 0.68},
			Setup: &CarSetup{
				UpdateCount: 3,
				Tyres:       []SetupTyre{{Wheel: WheelLF, ColdKpa: 165, HotKpa: 172.5}},
				RearWing:    &SetupValue{Name: "RearWingSetting", Text: "9 hole", Number: 9, Unit: "hole"},
				Values:      []SetupValue{{Group: "Chassis/Rear", Name: "CrossWeight", Text: "50.0%", Number: 50, Unit: "%"}},
			},
		}, new(Stint)},
		{"StintResult", StintResult{
			StintID:   "01997f3a-0000-7000-8000-000000000002",
			SessionID: ptr("01997f3a-0000-7000-8000-00000000000a"),
		}, new(StintResult)},
		{"Summary", Summary{
			Laps: 12, Incidents: 2, BestLapMs: 90118, AvgLapMs: 91402,
			ConsistencyPct: 94, TopSpeedKmh: 271,
			BestTrace: []TracePoint{{OffsetMs: 0, SpeedKmh: 205, Throttle: 100, Gear: 5, RPM: 7100}},
			Conditions: Conditions{
				Skies: 1, Wetness: 0, WindKmh: 8.5, Humidity: 41.25,
				TrackTempC: 34.5, AirTempC: 24.875,
			},
			CarState: CarState{
				FuelUsedL: 48.25, FuelLevelL: 12.5,
				TyreTempC: TyreTemps{LF: 82.1, RF: 84, LR: 79.625, RR: 81.25},
			},
			FinishedAt: ptr(at.Add(31 * time.Minute)),
		}, new(Summary)},
		{"TTSRequest", TTSRequest{Text: "Turn 4, more entry speed.", Voice: ptr("nova"), Lang: "en"}, new(TTSRequest)},
		{"Team", Team{ID: "01997f3a-0000-7000-8000-000000000009", Name: "Escudería Norte"}, new(Team)},
		{"TracePoint", TracePoint{
			OffsetMs: 12300, SpeedKmh: 211, Throttle: 98, Brake: 1, Gear: 5, RPM: 7400,
			Steer: -37, DistPct: 421, LatG: -152, LongG: 88, La: 4157311, Lo: 209044,
		}, new(TracePoint)},
		{"TyreTemps", TyreTemps{LF: 82.1, RF: 84, LR: 79.625, RR: 81.25}, new(TyreTemps)},
	}
}

// pinnedShape is one entry of testdata/shapes.json.
type pinnedShape struct {
	Type string          `json:"type"`
	Full json.RawMessage `json:"full"`
	Zero json.RawMessage `json:"zero"`
}

// TestJSONShape is the tag pin. Every type in this package is marshalled and
// compared against bytes committed to testdata/shapes.json, so a tag cannot be
// renamed, reordered, gained or lost without a diff that says so.
//
// This is not a formality. The client and the server are separate programs in
// separate repositories, and a tag changed on one side and not the other is a
// bug that compiles, passes both suites and only shows up as a field that is
// silently zero in production.
func TestJSONShape(t *testing.T) {
	t.Parallel()

	pinned := readShapes(t)
	for _, s := range shapes() {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			want, ok := pinned[s.name]
			r.True(ok, "%s has no pinned shape; run: go test ./wire -update", s.name)

			full, err := json.Marshal(s.full)
			r.NoError(err)
			r.Equal(string(want.Full), string(full),
				"the serialised shape of %s has changed, keys included", s.name)

			zero, err := json.Marshal(s.zero)
			r.NoError(err)
			r.Equal(string(want.Zero), string(zero),
				"the zero-value shape of %s has changed; an omitempty came or went", s.name)
		})
	}
}

// TestJSONRoundTrip decodes each pinned document back into its type and
// re-encodes it. A tag that marshals but does not unmarshal — a duplicate, or
// one on an unexported field — survives TestJSONShape and dies here.
func TestJSONRoundTrip(t *testing.T) {
	t.Parallel()

	pinned := readShapes(t)
	for _, s := range shapes() {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			dec := json.NewDecoder(bytes.NewReader(pinned[s.name].Full))
			dec.DisallowUnknownFields() // the server decodes with this on (D-3)

			into := newLike(s.full)
			r.NoError(dec.Decode(into), "%s does not decode its own pinned shape", s.name)

			again, err := json.Marshal(into)
			r.NoError(err)
			r.Equal(string(pinned[s.name].Full), string(again),
				"%s does not survive a decode and re-encode", s.name)
		})
	}
}

// TestEveryExportedStructIsPinned reads this package's own source and requires
// that every exported struct type appears in shapes(). Without it a type added
// next year is simply not tested, and the pin quietly stops covering the wire.
func TestEveryExportedStructIsPinned(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	declared := exportedStructs(t)
	r.NotEmpty(declared, "the source scan found nothing, so it proves nothing")

	pinnedNames := map[string]bool{}
	for _, s := range shapes() {
		pinnedNames[s.name] = true
	}

	for _, name := range declared {
		r.True(pinnedNames[name],
			"exported struct %s has no entry in shapes(); every wire type must be pinned", name)
	}
	for name := range pinnedNames {
		r.Contains(declared, name, "shapes() pins %s, which is no longer an exported struct", name)
	}
}

func exportedStructs(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()
	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, n, nil, parser.SkipObjectResolution)
		require.NoError(t, err)
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !ts.Name.IsExported() {
					continue
				}
				if _, ok := ts.Type.(*ast.StructType); ok {
					names = append(names, ts.Name.Name)
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

func readShapes(t *testing.T) map[string]pinnedShape {
	t.Helper()

	b, err := os.ReadFile(shapesPath)
	require.NoError(t, err)
	var list []pinnedShape
	require.NoError(t, json.Unmarshal(b, &list))

	out := make(map[string]pinnedShape, len(list))
	for _, p := range list {
		out[p.Type] = p
	}
	require.Len(t, out, len(list), "testdata/shapes.json pins a type twice")
	return out
}

// newLike returns the fresh zero value the shapes table already holds for v's
// type, which is a pointer and therefore something a decoder can write into.
// shapes() builds a new set on every call, so nothing is shared between tests.
func newLike(v any) any {
	for _, s := range shapes() {
		if fmt.Sprintf("%T", s.full) == fmt.Sprintf("%T", v) {
			return s.zero
		}
	}
	return nil
}

func writeShapes() error {
	if err := os.MkdirAll(filepath.Dir(shapesPath), 0o755); err != nil {
		return fmt.Errorf("testdata: %w", err)
	}
	var buf bytes.Buffer
	buf.WriteString("[\n")
	list := shapes()
	for i, s := range list {
		full, err := json.Marshal(s.full)
		if err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		zero, err := json.Marshal(s.zero)
		if err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		fmt.Fprintf(&buf, "{%q:%q,%q:%s,%q:%s}", "type", s.name, "full", full, "zero", zero)
		if i < len(list)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("]\n")
	return os.WriteFile(shapesPath, buf.Bytes(), 0o644)
}
