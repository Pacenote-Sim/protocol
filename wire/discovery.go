package wire

// Feature names one optional capability. A capability is present only when it is
// named in both [Discovery.Features] and [Me.Features]: the first says the
// server has it, the second says this driver is entitled to it. Anything not
// named is absent, and absent means a disabled button, never an error dialog.
type Feature string

// The v1 features. A client that meets a feature it does not know ignores it.
const (
	FeatureTelemetry   Feature = "telemetry"   // stints, laps, traces, summaries — the core
	FeatureReference   Feature = "reference"   // GET /reference, required by the coach's training mode
	FeatureLive        Feature = "live"        // POST /live, the fire-and-forget sample stream
	FeatureField       Feature = "field"       // POST /field, relaying the whole field
	FeatureTTS         Feature = "tts"         // POST /tts, spoken coaching
	FeatureSetups      Feature = "setups"      // language-model setup advice, operator's own API key
	FeatureCoach       Feature = "coach"       // language-model coaching prose, operator's own API key
	FeatureCompetition Feature = "competition" // championships, qualifying sessions, results
)

// Discovery is the one unauthenticated document that everything else follows
// from, served at GET /.well-known/sim-telemetry.json and cacheable for five
// minutes. A client resolves a typed host name to it over HTTPS, falling back to
// plain HTTP only for localhost, and never hardcodes anything it can read here.
//
// API is the base path for every other endpoint, conventionally "/api/v1".
// Name, ShortName, Logo and Accent are the white-label branding the client wears
// once paired; Accent is a CSS hex colour. MinClient is the lowest client
// version this server accepts, compared against the X-Client-Version header and
// answered with [CodeClientTooOld]. PairURI and PrivacyURI are shown on the
// pairing screen before the driver approves.
type Discovery struct {
	API        string    `json:"api"`
	Name       string    `json:"name"`
	ShortName  string    `json:"short_name"`
	Logo       string    `json:"logo"`
	Accent     string    `json:"accent"`
	Features   []Feature `json:"features"`
	Limits     Limits    `json:"limits"`
	MinClient  string    `json:"min_client"`
	PairURI    string    `json:"pair_uri"`
	PrivacyURI string    `json:"privacy_uri"`
}

// Has reports whether the server declares the feature. It is the server half of
// the two-sided check; the driver half is [Me.Has].
func (d Discovery) Has(f Feature) bool { return hasFeature(d.Features, f) }

// Limits replaces every hardcoded interval in the client. The capture period
// stays local — the client samples the simulator as fast as it likes — but how
// often anything is *sent* comes from here, and the server's own rate limiter is
// built from the same constants, so the two agree by construction.
//
//	TracePoints       the longest trace the server accepts, in samples; a
//	                  longer one is rejected outright (300 is typical)
//	LapsPerRequest    the most laps in one POST /stints/{id}/laps
//	LiveIntervalMs    minimum gap between POST /live calls
//	FieldIntervalMs   minimum gap between POST /field calls
//	SummaryIntervalMs minimum gap between PUT /stints/{id}/summary calls
//	MaxBodyBytes      the largest request body the server will read
type Limits struct {
	TracePoints       int `json:"trace_points"`
	LapsPerRequest    int `json:"laps_per_request"`
	LiveIntervalMs    int `json:"live_interval_ms"`
	FieldIntervalMs   int `json:"field_interval_ms"`
	SummaryIntervalMs int `json:"summary_interval_ms"`
	MaxBodyBytes      int `json:"max_body_bytes"`
}

func hasFeature(fs []Feature, want Feature) bool {
	for _, f := range fs {
		if f == want {
			return true
		}
	}
	return false
}
