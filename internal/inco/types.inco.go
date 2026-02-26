// Package inco implements a compile-time code injection engine.
//
// Directives:
//
//	// @inco: <expr>   — contract: expect expr to be true (generated: if !(expr))
//	// @if: <expr>     — same as if (generated: if expr)
//
// Both support action suffixes:
//
//	, -panic("msg")  , -return(x, y)  , -continue  , -break  , -log(args...)
//
// The default action is -panic with an auto-generated message.
package inco

// ---------------------------------------------------------------------------
// Engine types
// ---------------------------------------------------------------------------

// Overlay is the JSON structure consumed by `go build -overlay`.
type Overlay struct {
	Replace map[string]string `json:"Replace"`
}

// Manifest tracks source file hashes for incremental generation.
// Stored as .inco_cache/manifest.json.
type Manifest struct {
	Files map[string]ManifestEntry `json:"files"`
}

// ManifestEntry records the state of a single source file at last gen.
type ManifestEntry struct {
	SrcHash    string `json:"src_hash"`    // SHA-256 hex of source content
	ShadowPath string `json:"shadow_path"` // absolute path to shadow file
}
