// Package trace is the binary codec for lap traces: the format described by
// decision D-1 of docs/SERVER.md, and the reason this module exists.
//
// A 300-sample lap is twelve small integers per sample, about 30 kB as JSON.
// Stored as
//
//	structure of arrays → zig-zag delta → varint → zstd
//
// the same lap is one to two kilobytes, because speed, throttle and track
// position all move smoothly and a smooth channel deltas down to a handful of
// bits per sample. The twelve channels are laid down as twelve independent
// streams rather than interleaved, so each stream contains only values of one
// magnitude and one shape, which is what makes the deltas small and the
// entropy coder effective.
//
// # Format
//
// A blob is
//
//	byte 0      codec version, currently 1
//	bytes 1…k   the point count, as an unsigned varint
//	bytes k…    one zstd frame
//
// and the plaintext of that frame is the twelve streams, back to back, in the
// field order of [wire.TracePoint] — t, v, thr, brk, g, r, st, p, lg, og, la,
// lo. Each stream is exactly n values; each value is the difference from its
// predecessor in the same stream, the first taken against zero, zig-zag mapped
// to an unsigned integer and written as a varint. Streams carry no length of
// their own: the count in the header says how many values each holds, which is
// one number to bounds-check instead of twelve.
//
// The version byte is outside the compressed frame on purpose, so [Version] can
// answer without decompressing anything — the server stores it in a neighbouring
// smallint column and can migrate blobs without reading them. The point count is
// outside it too, so a blob claiming more than [MaxPoints] samples is refused
// before a single byte is decompressed.
//
// # Hostile input
//
// [Decode] is the only part of this module that reads bytes it did not write.
// Every length and every offset is bounds-checked, the decompressor is capped at
// [MaxPoints] worth of plaintext, an over-long input is rejected before zstd is
// invoked at all, and the decoded trace is put through the same [Validate] that
// [Encode] applies. A truncated, corrupt or hostile blob returns an error. It
// never panics, and it never returns a trace that would not have been legal to
// encode.
//
// # Allocation
//
// [Encode] performs one allocation: the returned slice, sized exactly. [Decode]
// performs none at all when the caller's destination slice is long enough,
// because it fills that slice in place and takes its scratch space from a
// [sync.Pool]. That is what lets the server's lap-ingest path decode forty laps
// without touching the heap.
//
// # Compatibility
//
// The golden vectors in testdata/golden are the contract, and testdata/README.md
// says what may and may not change about them.
package trace
