// Package trimx is a companion front-end for trim-1
// (github.com/caveman-ai/trim-1). It does not modify the trim-1 model; it
// fixes two context shapes trim-1 silently passes through or breaks:
//
//  1. Paraphrased natural-language asks on prose. trim-1 scores lines
//     against the query's own words, so "can I get my money back" scores
//     nothing on a refund-policy document and trim-1 returns the whole
//     document unchanged (0% cut, where the literal query "refund" cuts
//     97% on the same file). trimx expands the query with a small curated
//     synonym lexicon and merges the per-line keep probabilities (max)
//     before applying trim-1's own selection policy.
//  2. Pretty-printed JSON tool results. trim-1 is line-level, so it either
//     cannot isolate one record among thousands of identical-looking lines
//     (returns the input unchanged even at 395 KB) or, when it does cut,
//     emits syntactically invalid JSON. trimx detects JSON, segments it
//     into records, scores each record with trim-1, keeps whole records,
//     and re-emits valid JSON with explicit elision markers.
package trimx

import (
	"encoding/json"
	"strings"

	trim "github.com/caveman-ai/trim-1"
)

// Mode selects the compression path.
type Mode int

const (
	// ModeAuto picks ModeJSON when the input parses as JSON, else ModeText.
	ModeAuto Mode = iota
	ModeText
	ModeJSON
)

// Options mirrors trim.Options with a mode selector.
type Options struct {
	Point string
	Mode  Mode
}

// Result reports what happened.
type Result struct {
	Output               string
	KeptLines, Removed   int
	Unchanged            bool
	Reason               string
	Mode                 Mode
	ExpandedQuery        string // the query variant that added synonym terms; empty if none
}

// Compress compresses context for query, expanding the query and routing
// JSON input through record-level selection.
func Compress(context, query string, opts Options) Result {
	mode := opts.Mode
	if mode == ModeAuto {
		mode = ModeText
		trimmed := strings.TrimSpace(context)
		if len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{') && json.Valid([]byte(context)) {
			mode = ModeJSON
		}
	}
	if mode == ModeJSON {
		return compressJSON(context, query, opts)
	}
	return compressText(context, query, opts)
}

// compressText runs trim-1 with the original query, then rescues the
// paraphrase case: when trim-1 returns the input unchanged because too few
// lines scored (its "cut below the minimum" fallback), retry once with the
// synonym-expanded query and use that result when it actually cuts.
func compressText(context, query string, opts Options) Result {
	point := opts.Point
	if point == "" {
		point = trim.DefaultPoint
	}
	base := trim.Compress(context, query, trim.Options{Point: point})
	res := Result{
		Output:    base.Output,
		KeptLines: base.KeptLines,
		Removed:   base.RemovedLines,
		Unchanged: base.Unchanged,
		Reason:    base.Reason,
		Mode:      ModeText,
	}
	expanded := Expand(query)
	if !base.Unchanged || expanded == query {
		return res
	}
	alt := trim.Compress(context, expanded, trim.Options{Point: point})
	if alt.Unchanged {
		return res
	}
	return Result{
		Output:        alt.Output,
		KeptLines:     alt.KeptLines,
		Removed:       alt.RemovedLines,
		Mode:          ModeText,
		ExpandedQuery: expanded,
	}
}
