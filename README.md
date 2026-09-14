# github.com/pacenote-sim/protocol

The wire protocol between a sim-racing telemetry client and its server: the
version 1 data types, and the binary codec for lap traces.

```go
import (
	"github.com/pacenote-sim/protocol/trace"
	"github.com/pacenote-sim/protocol/wire"
)
```

The client and the server depend on this module by version. A `go.work` file one
directory up replaces it with a local checkout while both are worked on at the
same time.

| Package | What it is |
|---|---|
| `wire` | The v1 request and response types, with the JSON tags the API is defined in. Standard library only. |
| `trace` | The lap-trace codec: structure of arrays, zig-zag delta, varint, zstd. |

## Why it is a module of its own

The capture client and the server are two programs, built in two repositories,
by people who cannot see each other's code. Everything they agree on is here, and
nothing else is: there is no HTTP in this module, no database, no business rule,
no configuration. A type whose tags are a contract has to be readable in one
sitting, and a codec that both sides depend on has to be the same code on both
sides rather than the same specification implemented twice.

That is also why `trace/testdata/golden` exists. The client asserts that it
produces those bytes; the server asserts that it reads them; neither has to trust
the other's tests. Read `trace/testdata/README.md` before touching them.

## The rule

**Changing the wire format means bumping the codec version and regenerating the
golden vectors — in that order, deliberately, and never to make a red test
green.**

For `trace`, the format is the byte layout: `CodecVersion` in `trace/codec.go`, a
decoder kept for every version still in a database, and the vectors regenerated
last. For `wire`, the format is the JSON tags: `wire/testdata/shapes.json` pins
the serialised shape of every exported type, including the key order and every
`omitempty`, so a tag cannot drift silently. Regenerate it only alongside a
deliberate change to the API document.

A blob carries its codec version in its first byte, outside the compressed frame,
so `trace.Version` can read it without decompressing anything and a server can
migrate a table having read one byte of each row. That is the escape hatch the
format is designed around, and it is the reason a format change is survivable at
all.

## Using it

```go
blob, err := trace.Encode(lap)      // one allocation, sized exactly
...
pts, err = trace.Decode(blob, pts)  // no allocation at all, given room
```

`Encode` validates before it writes: a channel outside the range documented on
`wire.TracePoint`, an offset that does not increase, or more than
`trace.MaxPoints` samples is refused rather than stored and discovered later.
`Decode` validates again on the way out, so a truncated, corrupt or hostile blob
returns an error, never a panic, and never a trace that could not have been
encoded in the first place.

`Decode` fills the destination slice in place. Pass the slice from the previous
call — only its capacity matters — and a server ingesting forty laps allocates
nothing per lap.

## Budgets

From `docs/SERVER.md`, checked by `BenchmarkEncode` and `BenchmarkDecode`:

| Path | Budget |
|---|---|
| Encode, 300 samples | < 40 µs, 1 alloc/op |
| Decode, 300 samples | < 20 µs, 0 allocs/op, into a reused buffer |

```
go test ./trace -run XXX -bench 'Encode|Decode' -benchmem -count=6
```

## Checks

```
go build ./... && go vet ./...
go test -race -shuffle=on -count=1 ./...
go test -cover ./...
golangci-lint run ./...
gofumpt -l .
go test ./trace -run XXX -fuzz FuzzDecode    -fuzztime 30s
go test ./trace -run XXX -fuzz FuzzRoundTrip -fuzztime 30s
```

Dependencies: `klauspost/compress` for zstd, which is pure Go so the server stays
CGO-free, plus `testify` and `goleak` for the tests. Nothing else, and
`.golangci.yml` has a `depguard` rule that keeps it that way — `wire` may import
the standard library and nothing more.

## Licence

**GNU General Public License, version 3 or later.** Full text in `LICENSE`.

A program that imports `wire` or `trace` is covered by the GPL too, and has to be
passed on under the same terms. Implementing the format from the types and
`trace/testdata/golden`, without importing this module, does not.
