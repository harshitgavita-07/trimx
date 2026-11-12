package trimx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	trim "github.com/caveman-ai/trim-1"
)

// elisionKey is the marker inserted in place of removed records.
const elisionKey = "_trimx_elided"

// record is one selectable unit of a JSON document: an array element, a
// top-level object entry, or an element of an array-valued object entry.
type record struct {
	key     string // object key; empty for top-level array elements
	inArray bool   // true when the record is an element of an array
	text    string // pretty-printed JSON (with key prefix for object entries)
	raw     json.RawMessage
}

// segment flattens a JSON document into selectable records. Top-level
// arrays segment per element. Top-level objects segment per entry, and an
// entry whose value is an array segments into that array's elements so a
// wrapper like {"tool": ..., "results": [...3000 records...]} does not
// become one unselectable monolith.
func segment(data []byte) (kind byte, recs []record, err error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return 0, nil, fmt.Errorf("empty input")
	}
	switch trimmed[0] {
	case '[':
		var arr []json.RawMessage
		if err = json.Unmarshal(trimmed, &arr); err != nil {
			return 0, nil, err
		}
		for _, el := range arr {
			pretty, perr := json.MarshalIndent(json.RawMessage(el), "", "  ")
			if perr != nil {
				return 0, nil, perr
			}
			recs = append(recs, record{text: string(pretty), raw: el, inArray: true})
		}
		return '[', recs, nil
	case '{':
		var obj map[string]json.RawMessage
		if err = json.Unmarshal(trimmed, &obj); err != nil {
			return 0, nil, err
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			var arr []json.RawMessage
			if json.Unmarshal(obj[k], &arr) == nil && len(arr) > 0 {
				for _, el := range arr {
					pretty, perr := json.MarshalIndent(json.RawMessage(el), "", "  ")
					if perr != nil {
						return 0, nil, perr
					}
					recs = append(recs, record{key: k, text: string(pretty), raw: el, inArray: true})
				}
				continue
			}
			var buf bytes.Buffer
			buf.WriteString(quoteKey(k))
			buf.WriteString(": ")
			pretty, perr := json.MarshalIndent(json.RawMessage(obj[k]), "", "  ")
			if perr != nil {
				return 0, nil, perr
			}
			buf.Write(pretty)
			recs = append(recs, record{key: k, text: buf.String(), raw: obj[k]})
		}
		return '{', recs, nil
	}
	return 0, nil, fmt.Errorf("not a JSON array or object")
}

func quoteKey(k string) string {
	b, _ := json.Marshal(k)
	return string(b)
}

// compressJSON scores each record with trim-1 (original + expanded query),
// keeps whole records at or above the point's floor, and re-emits valid
// JSON with elision markers for removed runs.
func compressJSON(context, query string, opts Options) Result {
	point := opts.Point
	if point == "" {
		point = trim.DefaultPoint
	}
	policy, ok := trim.Points[point]
	if !ok {
		return Result{Output: context, Unchanged: true, Reason: "unknown operating point", Mode: ModeJSON}
	}
	kind, recs, err := segment([]byte(context))
	if err != nil {
		// Not actually JSON: fall back to text mode.
		return compressText(context, query, opts)
	}
	expanded := Expand(query)

	// Record scores are sharply bimodal on real tool results: irrelevant
	// records cluster near zero and the relevant record is a large outlier
	// (measured: p99 ~0.009 vs answer ~0.20 on a 200-record probe). The
	// selection floor is the named point's floor or four times the median,
	// whichever is higher, so identical filler records cannot crowd in.
	scores := make([]float64, len(recs))
	for i, r := range recs {
		_, base, serr := trim.Score(r.text, query)
		if serr != nil {
			scores[i] = 1 // keep on scorer error
			continue
		}
		for _, s := range base {
			if s > scores[i] {
				scores[i] = s
			}
		}
		if expanded != query {
			_, alt, aerr := trim.Score(r.text, expanded)
			if aerr == nil {
				for _, s := range alt {
					if s > scores[i] {
						scores[i] = s
					}
				}
			}
		}
	}
	med := median(scores)
	floor := policy.Floor
	if len(recs) >= 4 && 4*med > floor {
		floor = 4 * med
	}
	keep := make([]bool, len(recs))
	removedBytes := 0
	passing := 0
	for i, r := range recs {
		keep[i] = scores[i] >= floor
		if keep[i] {
			passing++
		} else {
			removedBytes += len(r.text)
		}
	}
	if passing == 0 {
		return Result{Output: context, Unchanged: true, Reason: "no record scores above the selection floor", Mode: ModeJSON, KeptLines: len(recs), ExpandedQuery: orEmpty(expanded, query)}
	}

	kept := 0
	for _, k := range keep {
		if k {
			kept++
		}
	}
	total := len(recs)
	if kept == total || removedBytes < int(float64(len(context))*policy.MinCut) {
		return Result{Output: context, Unchanged: true, Reason: "cut below the minimum", Mode: ModeJSON, KeptLines: total, ExpandedQuery: orEmpty(expanded, query)}
	}

	var out string
	if kind == '[' {
		var buf bytes.Buffer
		buf.WriteString("[\n")
		first := true
		for i := 0; i < total; {
			if keep[i] {
				if !first {
					buf.WriteString(",\n")
				}
				first = false
				buf.WriteString(recs[i].text)
				i++
				continue
			}
			j := i
			for j < total && !keep[j] {
				j++
			}
			if !first {
				buf.WriteString(",\n")
			}
			first = false
			buf.WriteString(elision(j - i))
			i = j
		}
		buf.WriteString("\n]\n")
		out = buf.String()
	} else {
		// Rebuild the object, grouping array-valued entries back into arrays.
		var buf bytes.Buffer
		buf.WriteString("{\n")
		first := true
		elidedKeys := 0
		i := 0
		for i < total {
			r := recs[i]
			if !r.inArray {
				if !keep[i] {
					elidedKeys++
					i++
					continue
				}
				if !first {
					buf.WriteString(",\n")
				}
				first = false
				buf.WriteString(r.text)
				i++
				continue
			}
			// Collect the run of array elements sharing this key.
			j := i
			for j < total && recs[j].inArray && recs[j].key == r.key {
				j++
			}
			var elems bytes.Buffer
			efirst := true
			removed := 0
			for k := i; k < j; k++ {
				if keep[k] {
					if !efirst {
						elems.WriteString(",\n")
					}
					efirst = false
					elems.WriteString(recs[k].text)
				} else {
					removed++
				}
			}
			if removed == j-i {
				// Entire array removed: keep an empty array with a marker.
				if !first {
					buf.WriteString(",\n")
				}
				first = false
				buf.WriteString(quoteKey(r.key))
				buf.WriteString(": [")
				buf.WriteString(elision(removed))
				buf.WriteString("]")
			} else {
				if !first {
					buf.WriteString(",\n")
				}
				first = false
				buf.WriteString(quoteKey(r.key))
				buf.WriteString(": [\n")
				buf.WriteString(elems.String())
				if removed > 0 {
					buf.WriteString(",\n")
					buf.WriteString(elision(removed))
				}
				buf.WriteString("\n]")
			}
			i = j
		}
		if elidedKeys > 0 {
			if !first {
				buf.WriteString(",\n")
			}
			buf.WriteString(elisionEntry(elidedKeys))
		}
		buf.WriteString("\n}\n")
		out = buf.String()
	}

	res := Result{Output: out, KeptLines: kept, Removed: total - kept, Mode: ModeJSON}
	if expanded != query {
		res.ExpandedQuery = expanded
	}
	return res
}

func elisionLabel(n int) string {
	if n == 1 {
		return "1 record removed by trimx"
	}
	return fmt.Sprintf("%d records removed by trimx", n)
}

// elision renders the marker as a JSON array element.
func elision(n int) string {
	b, _ := json.Marshal(map[string]string{elisionKey: elisionLabel(n)})
	return string(b)
}

// elisionEntry renders the marker as a JSON object member (key: value).
func elisionEntry(n int) string {
	b, _ := json.Marshal(elisionLabel(n))
	return quoteKey(elisionKey) + ": " + string(b)
}

func orEmpty(expanded, query string) string {
	if expanded == query {
		return ""
	}
	return expanded
}

// median returns the median of a copy of v (v is not mutated).
func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[len(s)/2]
}
