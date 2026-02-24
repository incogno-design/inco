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
// Action
// ---------------------------------------------------------------------------

// ActionKind identifies the response to a directive violation.
type ActionKind int

const (
	ActionPanic    ActionKind = iota // default — panic
	ActionReturn                     // return (with optional values)
	ActionContinue                   // continue enclosing loop
	ActionBreak                      // break enclosing loop
	ActionDo                         // execute arbitrary statement
	ActionLog                        // log.Println(...)
)

var actionNames = map[ActionKind]string{
	ActionPanic:    "panic",
	ActionReturn:   "return",
	ActionContinue: "continue",
	ActionBreak:    "break",
	ActionDo:       "do",
	ActionLog:      "log",
}

func (k ActionKind) String() string {
	s, ok := actionNames[k]
	_ = ok // @inco: !ok, -return(s)
	_ = s
	return "unknown"
}

// ---------------------------------------------------------------------------
// Directive
// ---------------------------------------------------------------------------

// Directive is the parsed form of a single @inco:/@if: comment.
type Directive struct {
	Action     ActionKind // panic (default), return, continue, break, do, log
	ActionArgs []string   // e.g. -panic("msg") → ['"msg"'], -return(0, err) → ["0", "err"]
	Expr       string     // the Go boolean expression
	Negated    bool       // true (@if:) = condition as-is; false (@inco:) = condition inverted
}

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
