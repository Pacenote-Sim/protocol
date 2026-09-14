package trace

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pacenote-sim/protocol/wire"
)

// TestEncodeDecodeRoundTrip walks the shapes a trace can take. Every case is
// asserted in both directions, because a codec that is wrong in one direction
// and wrong again in the other still round-trips.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()

	one := realisticLap(1)
	tests := []struct {
		name string
		pts  []wire.TracePoint
	}{
		{"nil", nil},
		{"empty", []wire.TracePoint{}},
		{"single point", one},
		{"two points", realisticLap(2)},
		{"a realistic lap", realisticLap(300)},
		{"at the cap", realisticLap(MaxPoints)},
		{"no gps fix", stripGPS(realisticLap(64))},
		{"every channel at its bounds", extremeTrace()},
		{"a long constant run", constantRun(512)},
		{"all zero but t", constantRun(8)},
		{"one millisecond apart", tightOffsets(256)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			blob, err := Encode(tt.pts)
			r.NoError(err)
			r.NotEmpty(blob, "even an empty trace produces a header")

			v, err := Version(blob)
			r.NoError(err)
			r.Equal(CodecVersion, v)

			got, err := Decode(blob, nil)
			r.NoError(err)
			r.Len(got, len(tt.pts))
			for i := range tt.pts {
				r.Equal(tt.pts[i], got[i], "point %d", i)
			}

			// Encoding is a function of the trace and nothing else: the same
			// input must give the same bytes on every call, or the golden
			// vectors would be meaningless.
			again, err := Encode(tt.pts)
			r.NoError(err)
			r.True(bytes.Equal(blob, again), "Encode is not deterministic")
		})
	}
}

// TestDecodeReusesTheDestination pins the contract the server's ingest path
// depends on: the destination slice is filled in place, its backing array is not
// swapped for a new one, and a shorter trace after a longer one leaves no trace
// of the longer one behind.
func TestDecodeReusesTheDestination(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	dst := make([]wire.TracePoint, 0, MaxPoints)
	before := &dst[:1][0]

	long, err := Encode(realisticLap(300))
	r.NoError(err)
	short, err := Encode(realisticLap(7))
	r.NoError(err)

	dst, err = Decode(long, dst)
	r.NoError(err)
	r.Len(dst, 300)
	r.Same(before, &dst[0], "Decode allocated a new array for a trace that fitted")

	dst, err = Decode(short, dst)
	r.NoError(err)
	r.Len(dst, 7)
	r.Same(before, &dst[0])
	r.Equal(realisticLap(7), dst)

	// A destination too small for the trace is grown, not overrun.
	small := make([]wire.TracePoint, 0, 2)
	got, err := Decode(long, small)
	r.NoError(err)
	r.Len(got, 300)
}

// TestDecodeErrors is every way a blob can be refused. Each case asserts the
// sentinel, so a caller can branch on "this row is corrupt" versus "this row is
// from a newer server" without matching on message text.
func TestDecodeErrors(t *testing.T) {
	t.Parallel()

	good, err := Encode(realisticLap(50))
	require.NoError(t, err)

	tests := []struct {
		name    string
		blob    []byte
		wantErr error
		wantMsg string
	}{
		{
			name:    "empty blob",
			blob:    nil,
			wantErr: ErrMalformed,
			wantMsg: "empty blob",
		},
		{
			name:    "version this build does not write",
			blob:    []byte{CodecVersion + 1, 0x00},
			wantErr: ErrVersion,
		},
		{
			name:    "version zero",
			blob:    []byte{0x00, 0x00},
			wantErr: ErrVersion,
		},
		{
			name:    "point count truncated away",
			blob:    []byte{CodecVersion},
			wantErr: ErrMalformed,
			wantMsg: "point count",
		},
		{
			name:    "point count overlong",
			blob:    append([]byte{CodecVersion}, bytes.Repeat([]byte{0xff}, 11)...),
			wantErr: ErrMalformed,
			wantMsg: "point count",
		},
		{
			name:    "more points than the cap",
			blob:    countClaim(MaxPoints + 1),
			wantErr: ErrTooManyPoints,
			wantMsg: "4097 points",
		},
		{
			name:    "a preposterous number of points",
			blob:    countClaim(math.MaxUint32),
			wantErr: ErrTooManyPoints,
		},
		{
			name:    "at the cap but with no payload",
			blob:    countClaim(MaxPoints),
			wantErr: ErrMalformed,
		},
		{
			name:    "not a zstd frame",
			blob:    append(countClaim(2), "notzstd"...),
			wantErr: ErrMalformed,
			wantMsg: "zstd",
		},
		{
			name:    "truncated mid frame",
			blob:    good[:len(good)-3],
			wantErr: ErrMalformed,
		},
		{
			name:    "a blob longer than any legal trace",
			blob:    make([]byte, MaxEncodedBytes+1),
			wantErr: ErrMalformed,
			wantMsg: "maximum",
		},
		{
			name:    "streams shorter than the count claims",
			blob:    reframe(t, countClaim(MaxPoints), rawOf(t, realisticLap(3))),
			wantErr: ErrMalformed,
			wantMsg: "ends at value",
		},
		{
			name:    "plaintext with bytes left over",
			blob:    reframe(t, countClaim(3), append(rawOf(t, realisticLap(3)), 0x00, 0x00)),
			wantErr: ErrMalformed,
			wantMsg: "trailing",
		},
		{
			name:    "a channel outside its documented range",
			blob:    reframe(t, countClaim(1), overRangeRaw(MaxSpeedKmh+1)),
			wantErr: ErrInvalidTrace,
			wantMsg: "v is 701",
		},
		{
			name:    "a delta wider than any channel",
			blob:    reframe(t, countClaim(1), hugeDeltaRaw()),
			wantErr: ErrInvalidTrace,
			wantMsg: "outside ±",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			dst := make([]wire.TracePoint, 4, 16)
			got, err := Decode(tt.blob, dst)
			r.Error(err)
			r.ErrorIs(err, tt.wantErr)
			r.Empty(got, "a failed Decode hands the buffer back emptied")
			if tt.wantMsg != "" {
				r.Contains(err.Error(), tt.wantMsg)
			}
		})
	}
}

// TestDecodeNeverPanics is the blunt version of the fuzz target's first
// property, kept as an ordinary test so it runs on every `go test`.
func TestDecodeNeverPanics(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	good, err := Encode(realisticLap(120))
	r.NoError(err)

	r.NotPanics(func() {
		for i := range good {
			for _, mask := range []byte{0x01, 0x80, 0xff} {
				bad := append([]byte(nil), good...)
				bad[i] ^= mask
				if got, err := Decode(bad, nil); err == nil {
					r.NoError(Validate(got))
				}
			}
		}
		for cut := range good {
			if got, err := Decode(good[:cut], nil); err == nil {
				r.NoError(Validate(got))
			}
		}
	})
}

func TestEncodeRejectsAnIllegalTrace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pts     []wire.TracePoint
		wantErr error
		wantMsg string
	}{
		{
			name:    "more points than the cap",
			pts:     constantRun(MaxPoints + 1),
			wantErr: ErrTooManyPoints,
		},
		{
			name:    "t goes backwards",
			pts:     []wire.TracePoint{{OffsetMs: 200}, {OffsetMs: 100}},
			wantErr: ErrInvalidTrace,
			wantMsg: "does not increase",
		},
		{
			name:    "t repeats",
			pts:     []wire.TracePoint{{OffsetMs: 100}, {OffsetMs: 100}},
			wantErr: ErrInvalidTrace,
			wantMsg: "does not increase",
		},
		{
			name:    "t negative",
			pts:     []wire.TracePoint{{OffsetMs: -1}},
			wantErr: ErrInvalidTrace,
			wantMsg: "t is -1",
		},
		{
			name:    "t past the hour",
			pts:     []wire.TracePoint{{OffsetMs: MaxOffsetMs + 1}},
			wantErr: ErrInvalidTrace,
			wantMsg: "t is 3600001",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			blob, err := Encode(tt.pts)
			r.Error(err)
			r.ErrorIs(err, tt.wantErr)
			r.Nil(blob)
			if tt.wantMsg != "" {
				r.Contains(err.Error(), tt.wantMsg)
			}
		})
	}
}

// TestValidateChannelBounds walks every channel off both ends of its range. The
// table is written out rather than generated so that a bound cannot be changed
// in validate.go and have the test follow it.
func TestValidateChannelBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		set  func(*wire.TracePoint, int)
		lo   int
		hi   int
		zero bool // the channel may legally hold zero
	}{
		{key: "v", set: func(p *wire.TracePoint, v int) { p.SpeedKmh = v }, lo: 0, hi: 700, zero: true},
		{key: "thr", set: func(p *wire.TracePoint, v int) { p.Throttle = v }, lo: 0, hi: 100, zero: true},
		{key: "brk", set: func(p *wire.TracePoint, v int) { p.Brake = v }, lo: 0, hi: 100, zero: true},
		{key: "g", set: func(p *wire.TracePoint, v int) { p.Gear = v }, lo: -1, hi: 10, zero: true},
		{key: "r", set: func(p *wire.TracePoint, v int) { p.RPM = v }, lo: 0, hi: 30000, zero: true},
		{key: "st", set: func(p *wire.TracePoint, v int) { p.Steer = v }, lo: -1000, hi: 1000, zero: true},
		{key: "p", set: func(p *wire.TracePoint, v int) { p.DistPct = v }, lo: 0, hi: 1000, zero: true},
		{key: "lg", set: func(p *wire.TracePoint, v int) { p.LatG = v }, lo: -1000, hi: 1000, zero: true},
		{key: "og", set: func(p *wire.TracePoint, v int) { p.LongG = v }, lo: -1000, hi: 1000, zero: true},
		{key: "la", set: func(p *wire.TracePoint, v int) { p.La = v }, lo: -9000000, hi: 9000000, zero: true},
		{key: "lo", set: func(p *wire.TracePoint, v int) { p.Lo = v }, lo: -18000000, hi: 18000000, zero: true},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			at := func(v int) []wire.TracePoint {
				p := wire.TracePoint{OffsetMs: 10}
				tt.set(&p, v)
				return []wire.TracePoint{p}
			}

			r.NoError(Validate(at(tt.lo)), "%s must accept its lower bound", tt.key)
			r.NoError(Validate(at(tt.hi)), "%s must accept its upper bound", tt.key)
			if tt.zero {
				r.NoError(Validate(at(0)))
			}

			under := Validate(at(tt.lo - 1))
			r.ErrorIs(under, ErrInvalidTrace)
			r.Contains(under.Error(), tt.key+" is ")

			over := Validate(at(tt.hi + 1))
			r.ErrorIs(over, ErrInvalidTrace)
			r.Contains(over.Error(), tt.key+" is ")

			// And the bound is enforced through Encode, not only by Validate.
			_, err := Encode(at(tt.hi + 1))
			r.ErrorIs(err, ErrInvalidTrace)
		})
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()

	t.Run("reads the head of a real blob", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		blob, err := Encode(realisticLap(10))
		r.NoError(err)
		v, err := Version(blob)
		r.NoError(err)
		r.Equal(CodecVersion, v)
	})

	t.Run("does not decompress", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		// Nothing but the first byte is readable, and Version still answers:
		// that is the property the server's migration path relies on.
		v, err := Version([]byte{7})
		r.NoError(err)
		r.Equal(7, v)
	})

	t.Run("refuses an empty blob", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		_, err := Version(nil)
		r.ErrorIs(err, ErrMalformed)
	})
}

func TestZigZagIsTotalAndInvertible(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	for _, v := range []int64{0, 1, -1, 2, -2, 127, -128, 1 << 20, -(1 << 20), math.MaxInt64, math.MinInt64} {
		r.Equal(v, unzigzag(zigzag(v)), "zigzag round trip for %d", v)
	}
	r.Equal(uint64(0), zigzag(0))
	r.Equal(uint64(1), zigzag(-1))
	r.Equal(uint64(2), zigzag(1))
	// Total in the other direction: every uint64 maps back to some int64, which
	// is what keeps a hostile varint from being a special case.
	for _, u := range []uint64{0, 1, 2, 3, math.MaxUint64, math.MaxUint64 - 1} {
		r.Equal(u, zigzag(unzigzag(u)))
	}
}

// TestErrorsAreDistinguishable keeps the sentinels apart: a caller that treats
// ErrVersion as ErrMalformed would migrate a perfectly good old blob into the
// bin, so the two must never satisfy errors.Is for one another.
func TestErrorsAreDistinguishable(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	all := []error{ErrVersion, ErrMalformed, ErrTooManyPoints, ErrInvalidTrace}
	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}
			r.NotErrorIs(a, b)
		}
		r.True(strings.HasPrefix(a.Error(), "trace: "), "%v should name its package", a)
	}
}

func tightOffsets(n int) []wire.TracePoint {
	pts := make([]wire.TracePoint, n)
	for i := range pts {
		pts[i] = wire.TracePoint{OffsetMs: i, SpeedKmh: 100, Gear: 3, RPM: 6000}
	}
	return pts
}

// rawOf returns the uncompressed twelve-stream plaintext of a trace, by
// encoding it and decompressing the frame again. Tests that need to craft a
// hostile blob start from a real one.
func rawOf(t *testing.T, pts []wire.TracePoint) []byte {
	t.Helper()
	blob, err := Encode(pts)
	require.NoError(t, err)
	_, body, err := header(blob)
	require.NoError(t, err)
	dec, err := decoder()
	require.NoError(t, err)
	raw, err := dec.DecodeAll(body, nil)
	require.NoError(t, err)
	return raw
}

// reframe compresses raw and hangs it off the given header, which is how every
// crafted-payload case in this file is built.
func reframe(t *testing.T, head, raw []byte) []byte {
	t.Helper()
	enc, err := encoder()
	require.NoError(t, err)
	return enc.EncodeAll(raw, append([]byte(nil), head...))
}

// overRangeRaw is the plaintext of one point whose speed is v and whose every
// other channel is zero.
func overRangeRaw(v int) []byte {
	raw := binary.AppendUvarint(nil, zigzag(0)) // t
	raw = binary.AppendUvarint(raw, zigzag(int64(v)))
	for range 10 {
		raw = binary.AppendUvarint(raw, zigzag(0))
	}
	return raw
}

// hugeDeltaRaw is the plaintext of one point whose first delta is wider than any
// channel could hold, which the decoder must refuse while it is accumulating
// rather than after it has narrowed the value to an int.
func hugeDeltaRaw() []byte {
	raw := binary.AppendUvarint(nil, zigzag(math.MaxInt64/2))
	for range 11 {
		raw = binary.AppendUvarint(raw, zigzag(0))
	}
	return raw
}

// TestDecodeRejectsEachStream walks the twelve stream loops one at a time, with
// the plaintext truncated inside that stream and then with a delta too wide for
// any channel. Every loop has the same two guards and a table like this is the
// only way to know that all twenty-four of them are wired up: a copy-paste that
// left one loop reporting its neighbour's index would pass every other test in
// this file.
func TestDecodeRejectsEachStream(t *testing.T) {
	t.Parallel()

	keys := []string{"t", "v", "thr", "brk", "g", "r", "st", "p", "lg", "og", "la", "lo"}
	const n = 3

	for s, key := range keys {
		t.Run("truncated in "+key, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			raw, offs := craftRaw(n, func(_, j int) int64 { return int64(j) })
			blob := reframe(t, countClaim(n), raw[:offs[s][1]])

			_, err := Decode(blob, nil)
			r.ErrorIs(err, ErrMalformed)
			r.Contains(err.Error(), fmt.Sprintf("stream %d (%s)", s, key))
			r.Contains(err.Error(), "value 1")
		})

		t.Run("out of range in "+key, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			raw, _ := craftRaw(n, func(stream, j int) int64 {
				if stream == s && j == 0 {
					return math.MaxInt64 / 4
				}
				return int64(j)
			})
			blob := reframe(t, countClaim(n), raw)

			_, err := Decode(blob, nil)
			r.ErrorIs(err, ErrInvalidTrace)
			r.Contains(err.Error(), key+" is ")
			r.Contains(err.Error(), "outside ±")
		})
	}
}

// TestDecodeRejectsPlaintextTooWideForItsCount covers the guard between the
// decompressor and the parser: a blob whose header claims few points but whose
// frame expands to far more bytes than those points could occupy.
func TestDecodeRejectsPlaintextTooWideForItsCount(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	raw, _ := craftRaw(1, func(_, _ int) int64 { return 0 })
	raw = append(raw, make([]byte, 1024)...)
	blob := reframe(t, countClaim(1), raw)

	_, err := Decode(blob, nil)
	r.ErrorIs(err, ErrMalformed)
	r.Contains(err.Error(), "plaintext bytes for 1 points")
}

// craftRaw builds the twelve-stream plaintext by hand and reports where every
// value starts, which is what lets a test cut the bytes in the middle of a
// chosen stream.
func craftRaw(n int, value func(stream, j int) int64) (raw []byte, offsets [][]int) {
	offsets = make([][]int, 12)
	for s := range 12 {
		offsets[s] = make([]int, n)
		prev := int64(0)
		for j := range n {
			offsets[s][j] = len(raw)
			v := value(s, j)
			raw = binary.AppendUvarint(raw, zigzag(v-prev))
			prev = v
		}
	}
	return raw, offsets
}
