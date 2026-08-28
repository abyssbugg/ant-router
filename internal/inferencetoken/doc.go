// Package inferencetoken manages the unprivileged credential used by local
// model clients to access the inference data plane.
//
// It intentionally defines a token type and store separate from controltoken:
// possession of an inference token must never grant configuration authority,
// and neither package may accept the other's values or storage paths. The
// persistence mechanics mirror the reviewed control-token implementation so
// both credentials receive the same durability and platform protections.
package inferencetoken
