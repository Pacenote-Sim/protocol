package trace

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"

	"github.com/pacenote-sim/protocol/wire"
)

// CodecVersion is the version byte at the head of every blob this package
// writes. It is the number the server keeps in its trace_codec column, and the
// only value [Decode] accepts.
//
// Bumping it is the one way the format is allowed to change, and it is a change
// that must be made deliberately: see testdata/README.md.
const CodecVersion = 1

// MaxEncodedBytes is the largest blob [Decode] will look at. A legal trace at
// [MaxPoints] cannot exceed roughly half of it even when every channel is
// incompressible noise, so this only ever rejects something that was never going
// to decode — and it rejects it before the decompressor is handed anything.
const MaxEncodedBytes = 1 << 20

// maxDecodedBytes is the plaintext ceiling given to the decompressor: twelve
// streams of [MaxPoints] values, each value at its widest possible varint.
const maxDecodedBytes = MaxPoints * 12 * binary.MaxVarintLen64

// encoderWindow is the zstd window the frames are written with. It is larger
// than the largest plaintext this codec can produce, so the whole trace is
// always in the window and the match finder never gives up a match to it.
//
// It is part of the byte-for-byte output of [Encode] and therefore part of what
// the golden vectors pin: changing it changes every committed .bin file.
const encoderWindow = 1 << 19

// maxChannel is the widest magnitude any channel may legally hold. Decoding
// bounds every accumulated value against it, which both rejects a hostile blob
// early and keeps the int64 the deltas are accumulated in inside the range of an
// int32 before it is narrowed.
const maxChannel = MaxLon

// ErrVersion reports a blob whose codec version this build does not know. A
// server holding blobs of an older version keeps the older decoder, or migrates
// them; it never guesses.
var ErrVersion = errors.New("trace: unsupported codec version")

// ErrMalformed reports a blob that is truncated, corrupt, or carries bytes the
// format does not account for. It is the answer to every hostile input that is
// not simply too long.
var ErrMalformed = errors.New("trace: malformed blob")

// Encode writes pts as one self-describing blob.
//
// It validates first: a trace that would not survive the round trip — a channel
// out of range, an offset that does not increase, more than [MaxPoints] samples
// — is refused here rather than stored and discovered later by whoever reads it
// back. A nil or empty slice is legal and encodes to a header and an empty
// frame.
//
// Exactly one allocation is performed, the returned slice, and it is sized to
// fit. The scratch space the twelve streams are built in comes from a pool, so a
// server encoding lap after lap reuses the same few kilobytes for all of them.
func Encode(pts []wire.TracePoint) ([]byte, error) {
	if err := Validate(pts); err != nil {
		return nil, err
	}
	enc, err := encoder()
	if err != nil {
		return nil, err
	}

	rawBuf := scratch.get()
	outBuf := scratch.get()
	defer func() {
		scratch.put(rawBuf)
		scratch.put(outBuf)
	}()

	raw := (*rawBuf)[:0]
	var prev, v int64

	// The twelve streams are written out one loop each rather than driven from a
	// table of field accessors. A table would need either reflection or a
	// function value per channel, and this is the path that runs once per lap on
	// the client and forty times per request on the server: an indirect call per
	// value costs more than the varint it is fetching. The loops are generated
	// in shape and identical in body, so a mistake in one is visible against its
	// eleven neighbours, and TestDecodeRejectsEachStream walks all twelve.

	// stream 0: t
	prev = 0
	for j := range pts {
		v = int64(pts[j].OffsetMs)
		raw = binary.AppendUvarint(raw, zigzag(v-prev))
		prev = v
	}

	// stream 1: v
	prev = 0
	for j := range pts {
		v = int64(pts[j].SpeedKmh)
		raw = binary.AppendUvarint(raw, zigzag(v-prev))
		prev = v
	}

	// stream 2: thr
	prev = 0
	for j := range pts {
		v = int64(pts[j].Throttle)
		raw = binary.AppendUvarint(raw, zigzag(v-prev))
		prev = v
	}

	// stream 3: brk
	prev = 0
	for j := range pts {
		v = int64(pts[j].Brake)
		raw = binary.AppendUvarint(raw, zigzag(v-prev))
		prev = v
	}

	// stream 4: g
	prev = 0
	for j := range pts {
		v = int64(pts[j].Gear)
		raw = binary.AppendUvarint(raw, zigzag(v-prev))
		prev = v
	}

	// stream 5: r
	prev = 0
	for j := range pts {
		v = int64(pts[j].RPM)
		raw = binary.AppendUvarint(raw, zigzag(v-prev))
		prev = v
	}

	// stream 6: st
	prev = 0
	for j := range pts {
		v = int64(pts[j].Steer)
		raw = binary.AppendUvarint(raw, zigzag(v-prev))
		prev = v
	}

	// stream 7: p
	prev = 0
	for j := range pts {
		v = int64(pts[j].DistPct)
		raw = binary.AppendUvarint(raw, zigzag(v-prev))
		prev = v
	}

	// stream 8: lg
	prev = 0
	for j := range pts {
		v = int64(pts[j].LatG)
		raw = binary.AppendUvarint(raw, zigzag(v-prev))
		prev = v
	}

	// stream 9: og
	prev = 0
	for j := range pts {
		v = int64(pts[j].LongG)
		raw = binary.AppendUvarint(raw, zigzag(v-prev))
		prev = v
	}

	// stream 10: la
	prev = 0
	for j := range pts {
		v = int64(pts[j].La)
		raw = binary.AppendUvarint(raw, zigzag(v-prev))
		prev = v
	}

	// stream 11: lo
	prev = 0
	for j := range pts {
		v = int64(pts[j].Lo)
		raw = binary.AppendUvarint(raw, zigzag(v-prev))
		prev = v
	}

	*rawBuf = raw

	out := (*outBuf)[:0]
	out = append(out, CodecVersion)
	out = binary.AppendUvarint(out, uint64(len(pts)))
	out = enc.EncodeAll(raw, out)
	*outBuf = out

	// The one allocation, sized exactly: EncodeAll's buffer is a pooled
	// over-estimate and handing it out would keep the over-estimate alive for
	// as long as the caller keeps the blob.
	blob := make([]byte, len(out))
	copy(blob, out)
	return blob, nil
}

// Version reads the codec version from the head of a blob without decompressing
// it, so a caller can route an old blob to an old decoder, or migrate a table,
// having read one byte of each row.
func Version(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, fmt.Errorf("%w: empty blob", ErrMalformed)
	}
	return int(b[0]), nil
}

// Decode reads a blob back into dst and returns the result.
//
// dst is reused: when it has room for the trace, Decode fills it in place and
// allocates nothing at all, which is what the server's ingest path depends on.
// Pass the slice from the previous call — its length is irrelevant, only its
// capacity is used — and keep the one Decode returns for the call after that.
//
// Every length and offset in the blob is bounds-checked, the decompressor is
// capped, and the result is put through [Validate] before it is returned, so a
// truncated, corrupt or hostile blob produces an error and never a panic and
// never a trace that could not have been encoded. On any error dst is returned
// truncated to zero length rather than dropped, so the caller keeps its buffer.
//
// A trace of no samples comes back as a zero-length slice, which is the caller's
// own slice emptied and is therefore nil if the caller passed nil. The wire
// format does not distinguish a nil trace from an empty one and neither does
// this: compare lengths, not slices.
func Decode(b []byte, dst []wire.TracePoint) ([]wire.TracePoint, error) {
	n, body, err := header(b)
	if err != nil {
		return dst[:0], err
	}

	dec, err := decoder()
	if err != nil {
		return dst[:0], err
	}

	rawBuf := scratch.get()
	defer scratch.put(rawBuf)

	raw, err := dec.DecodeAll(body, (*rawBuf)[:0])
	if raw != nil {
		*rawBuf = raw
	}
	if err != nil {
		return dst[:0], fmt.Errorf("%w: zstd: %w", ErrMalformed, err)
	}
	if limit := n * 12 * binary.MaxVarintLen64; len(raw) > limit {
		return dst[:0], fmt.Errorf("%w: %d plaintext bytes for %d points, maximum %d",
			ErrMalformed, len(raw), n, limit)
	}

	if cap(dst) < n {
		dst = make([]wire.TracePoint, n)
	} else {
		dst = dst[:n]
	}

	off := 0
	var prev int64

	// Twelve loops again, in the same order [Encode] laid them down, for the
	// same reason. Each reads n deltas, accumulates them, and bounds the result
	// before narrowing it to an int; the two guards inside every loop are what
	// make a truncated or hostile plaintext an error rather than a panic.

	// stream 0: t
	prev = 0
	for j := range n {
		d, k := binary.Uvarint(raw[off:])
		if k <= 0 {
			return dst[:0], truncated(0, "t", j)
		}
		off += k
		prev += unzigzag(d)
		if prev < -maxChannel || prev > maxChannel {
			return dst[:0], unbounded(j, "t", prev)
		}
		dst[j].OffsetMs = int(prev)
	}

	// stream 1: v
	prev = 0
	for j := range n {
		d, k := binary.Uvarint(raw[off:])
		if k <= 0 {
			return dst[:0], truncated(1, "v", j)
		}
		off += k
		prev += unzigzag(d)
		if prev < -maxChannel || prev > maxChannel {
			return dst[:0], unbounded(j, "v", prev)
		}
		dst[j].SpeedKmh = int(prev)
	}

	// stream 2: thr
	prev = 0
	for j := range n {
		d, k := binary.Uvarint(raw[off:])
		if k <= 0 {
			return dst[:0], truncated(2, "thr", j)
		}
		off += k
		prev += unzigzag(d)
		if prev < -maxChannel || prev > maxChannel {
			return dst[:0], unbounded(j, "thr", prev)
		}
		dst[j].Throttle = int(prev)
	}

	// stream 3: brk
	prev = 0
	for j := range n {
		d, k := binary.Uvarint(raw[off:])
		if k <= 0 {
			return dst[:0], truncated(3, "brk", j)
		}
		off += k
		prev += unzigzag(d)
		if prev < -maxChannel || prev > maxChannel {
			return dst[:0], unbounded(j, "brk", prev)
		}
		dst[j].Brake = int(prev)
	}

	// stream 4: g
	prev = 0
	for j := range n {
		d, k := binary.Uvarint(raw[off:])
		if k <= 0 {
			return dst[:0], truncated(4, "g", j)
		}
		off += k
		prev += unzigzag(d)
		if prev < -maxChannel || prev > maxChannel {
			return dst[:0], unbounded(j, "g", prev)
		}
		dst[j].Gear = int(prev)
	}

	// stream 5: r
	prev = 0
	for j := range n {
		d, k := binary.Uvarint(raw[off:])
		if k <= 0 {
			return dst[:0], truncated(5, "r", j)
		}
		off += k
		prev += unzigzag(d)
		if prev < -maxChannel || prev > maxChannel {
			return dst[:0], unbounded(j, "r", prev)
		}
		dst[j].RPM = int(prev)
	}

	// stream 6: st
	prev = 0
	for j := range n {
		d, k := binary.Uvarint(raw[off:])
		if k <= 0 {
			return dst[:0], truncated(6, "st", j)
		}
		off += k
		prev += unzigzag(d)
		if prev < -maxChannel || prev > maxChannel {
			return dst[:0], unbounded(j, "st", prev)
		}
		dst[j].Steer = int(prev)
	}

	// stream 7: p
	prev = 0
	for j := range n {
		d, k := binary.Uvarint(raw[off:])
		if k <= 0 {
			return dst[:0], truncated(7, "p", j)
		}
		off += k
		prev += unzigzag(d)
		if prev < -maxChannel || prev > maxChannel {
			return dst[:0], unbounded(j, "p", prev)
		}
		dst[j].DistPct = int(prev)
	}

	// stream 8: lg
	prev = 0
	for j := range n {
		d, k := binary.Uvarint(raw[off:])
		if k <= 0 {
			return dst[:0], truncated(8, "lg", j)
		}
		off += k
		prev += unzigzag(d)
		if prev < -maxChannel || prev > maxChannel {
			return dst[:0], unbounded(j, "lg", prev)
		}
		dst[j].LatG = int(prev)
	}

	// stream 9: og
	prev = 0
	for j := range n {
		d, k := binary.Uvarint(raw[off:])
		if k <= 0 {
			return dst[:0], truncated(9, "og", j)
		}
		off += k
		prev += unzigzag(d)
		if prev < -maxChannel || prev > maxChannel {
			return dst[:0], unbounded(j, "og", prev)
		}
		dst[j].LongG = int(prev)
	}

	// stream 10: la
	prev = 0
	for j := range n {
		d, k := binary.Uvarint(raw[off:])
		if k <= 0 {
			return dst[:0], truncated(10, "la", j)
		}
		off += k
		prev += unzigzag(d)
		if prev < -maxChannel || prev > maxChannel {
			return dst[:0], unbounded(j, "la", prev)
		}
		dst[j].La = int(prev)
	}

	// stream 11: lo
	prev = 0
	for j := range n {
		d, k := binary.Uvarint(raw[off:])
		if k <= 0 {
			return dst[:0], truncated(11, "lo", j)
		}
		off += k
		prev += unzigzag(d)
		if prev < -maxChannel || prev > maxChannel {
			return dst[:0], unbounded(j, "lo", prev)
		}
		dst[j].Lo = int(prev)
	}

	if off != len(raw) {
		return dst[:0], fmt.Errorf("%w: %d trailing plaintext bytes", ErrMalformed, len(raw)-off)
	}
	if err := Validate(dst); err != nil {
		return dst[:0], err
	}
	return dst, nil
}

// header reads the version byte and the point count and returns the count and
// the compressed remainder. Everything it can reject, it rejects before any
// decompression happens.
func header(b []byte) (n int, body []byte, err error) {
	if len(b) > MaxEncodedBytes {
		return 0, nil, fmt.Errorf("%w: %d bytes, maximum %d", ErrMalformed, len(b), MaxEncodedBytes)
	}
	v, err := Version(b)
	if err != nil {
		return 0, nil, err
	}
	if v != CodecVersion {
		return 0, nil, fmt.Errorf("%w: version %d, this build writes and reads %d",
			ErrVersion, v, CodecVersion)
	}
	count, k := binary.Uvarint(b[1:])
	if k <= 0 {
		return 0, nil, fmt.Errorf("%w: point count is truncated or overlong", ErrMalformed)
	}
	if count > MaxPoints {
		return 0, nil, fmt.Errorf("%w: blob claims %d points, maximum %d",
			ErrTooManyPoints, count, MaxPoints)
	}
	return int(count), b[1+k:], nil
}

// truncated and unbounded build the two errors the twelve decode loops can
// produce. They are functions and not inline fmt.Errorf calls so that the loops
// themselves stay small enough for the compiler to keep them tight: this is the
// path that decodes forty laps while a request is waiting.
func truncated(stream int, key string, j int) error {
	return fmt.Errorf("%w: stream %d (%s) ends at value %d of the trace",
		ErrMalformed, stream, key, j)
}

func unbounded(j int, key string, got int64) error {
	return fmt.Errorf("%w: point %d: %s is %d, outside ±%d",
		ErrInvalidTrace, j, key, got, maxChannel)
}

// zigzag maps a signed delta onto an unsigned one with small magnitudes staying
// small: 0, −1, 1, −2 become 0, 1, 2, 3. Without it every negative delta would
// varint to ten bytes, and half the deltas of a steering trace are negative.
func zigzag(v int64) uint64 {
	// The two conversions are the zig-zag mapping itself: the shifted value is
	// reinterpreted, not narrowed, and every int64 has a bit pattern that is a
	// valid uint64. [unzigzag] maps it back exactly, which TestZigZagIsTotal-
	// AndInvertible asserts over both extremes of the range.
	return uint64(v<<1) ^ uint64(v>>63) //nolint:gosec // G115: a reinterpretation of the bits, not a narrowing; see above.
}

// unzigzag is the inverse of [zigzag]. It is total: every uint64 maps back to
// some int64, which matters because the input may be hostile.
func unzigzag(u uint64) int64 {
	return int64(u>>1) ^ -int64(u&1)
}

// scratch holds the plaintext buffers of both directions. Both are the same
// shape — one flat byte slice of the twelve streams — so one pool serves both,
// and a server that encodes and decodes reuses the same buffers for each.
//
// The initial capacity covers a 300-point lap without growing; a longer one
// grows the buffer once and keeps it.
var scratch bufPool

// scratchInitial covers a 300-point lap without growing. A longer trace grows
// the buffer once and the pool keeps it grown.
const scratchInitial = 8 << 10

// bufPool is a [sync.Pool] of byte buffers with the type assertion in one place.
// The assertion cannot fail — nothing but a *[]byte is ever put in — but this
// package promises never to panic, and "cannot fail" is not a thing to promise
// on behalf of a pool that a future caller might share.
type bufPool struct{ p sync.Pool }

func (bp *bufPool) get() *[]byte {
	if b, ok := bp.p.Get().(*[]byte); ok && b != nil {
		return b
	}
	b := make([]byte, 0, scratchInitial)
	return &b
}

func (bp *bufPool) put(b *[]byte) { bp.p.Put(b) }

var encoder = sync.OnceValues(func() (*zstd.Encoder, error) {
	e, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedDefault),
		zstd.WithEncoderCRC(true),
		zstd.WithWindowSize(encoderWindow),
	)
	if err != nil {
		return nil, fmt.Errorf("trace: zstd encoder: %w", err)
	}
	return e, nil
})

var decoder = sync.OnceValues(func() (*zstd.Decoder, error) {
	d, err := zstd.NewReader(nil,
		// Three separate ceilings, because they bound three separate things.
		// MaxMemory caps the plaintext a frame may expand to. MaxWindow caps
		// the buffer a frame may ask the decoder to allocate before it expands
		// to anything at all — the library clamps it to MaxMemory anyway, and it
		// is written out because a decompression bomb is a memory amplification
		// before it is a size one. Lowmem false trades a fixed buffer for the
		// per-call allocations that would otherwise show up in the budget.
		zstd.WithDecoderMaxMemory(maxDecodedBytes),
		zstd.WithDecoderMaxWindow(encoderWindow),
		zstd.WithDecoderLowmem(false),
		zstd.WithDecoderConcurrency(0),
	)
	if err != nil {
		return nil, fmt.Errorf("trace: zstd decoder: %w", err)
	}
	return d, nil
})
