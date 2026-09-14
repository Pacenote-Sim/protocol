// Package wire holds the version 1 data types of the sim telemetry API, exactly
// as docs/API-V1.md defines them.
//
// It is the shared vocabulary of two programs built independently in separate
// repositories: the capture client that produces the data and the server that
// stores it. Nothing here does any work — there is no HTTP, no database and no
// business rule in this package, only the shape of the bytes that cross between
// them. That is deliberate: a type whose JSON tags are the contract must be
// readable in one sitting.
//
// Three rules hold for every type below.
//
//  1. The JSON tag is the contract, not the Go field name. Renaming a Go field
//     is free; changing a tag is a wire-format change and breaks every deployed
//     client. TestJSONShape pins the serialised shape of every type in this
//     package against testdata/shapes.json so a tag cannot drift silently.
//
//  2. Times are RFC 3339 with an offset, and durations are integer
//     milliseconds. time.Time marshals to RFC 3339 by itself; an optional time
//     is a *time.Time so that "absent" and "the zero instant" stay distinct.
//
//  3. Ids the client generates are UUIDv7 strings, so they sort by creation
//     time. This package does not generate or validate them: it carries them.
//
// The integer scaling of [TracePoint] is shared with the client's own
// telemetry.TracePoint byte for byte. See that type's documentation for the
// units, and package github.com/pacenote-sim/protocol/trace for the binary codec that puts a
// slice of them on the wire.
package wire
