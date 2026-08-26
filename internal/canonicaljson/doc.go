// Package canonicaljson parses bounded immutable JSON values and emits a
// deterministic representation. It rejects invalid UTF-8, duplicate object
// keys, excessive depth or size, and trailing top-level values. Object keys
// are sorted, array order and validated number lexemes are preserved, and
// callers receive defensive byte copies.
package canonicaljson
