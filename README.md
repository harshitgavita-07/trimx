# trimx

A companion front-end for [trim-1](https://github.com/caveman-ai/trim-1)
(Caveman's 1.2 MB context-trimming model) that improves two context
shapes where trim-1's line-level design gives little or no usable
savings. trim-1's model is untouched; trimx sits in front of it and uses
its public `Score` / `Compress` API and its own named operating points.

These are improvement opportunities, not defect claims: trim-1
documents that it is line-level ("It keeps or drops whole lines") and
that it returns input unchanged when a cut is too small to matter. trimx
adds the two layers that design leaves out.

## The gaps (reproduced against trim-1 v0.1.0, the current and only
release, last upstream commit 2026-10-29; probed 2026-11-12)

**1. Paraphrased asks return the input unchanged on prose.** trim-1
scores lines against the query's own words. On a 20.8 KB policy
document:

```
$ trim --query "what is the refund policy" policy.md
trim: kept 8 lines, removed 354, 20840 -> 568 bytes        # works
$ trim --query "can I get my money back" policy.md
trim: unchanged (cut below the minimum)                    # 0% cut
$ trim --query "how do returns work" policy.md
trim: unchanged (cut below the minimum)                    # 0% cut
```

Agents paraphrase constantly, so the savings disappear exactly when the
ask is phrased the way people (and agents) actually phrase it. The
fallback itself is safe (nothing is lost), but the stderr message ("cut
below the minimum") reads as a file-size issue while the file is
identical — only the query wording changed. trim-1's model card
discloses multi-hop entity chains as a limit; single-hop synonymy like
this is not in the list.

**2. Pretty-printed JSON tool results get no usable cut.** A 395 KB
JSON tool envelope (3,000 order records, one refunded) — a common agent
context shape:

```
$ trim --query "which order was refunded and why" big.json
trim: unchanged (cut below the minimum)        # 0% cut, answer inside
$ trim --query "damaged" big.json | python3 -m json.tool
json.decoder.JSONDecodeError                   # line-level cut drops braces/keys
```

Line-level scoring cannot isolate one record among thousands of
identical-looking lines, and a line-level cut of pretty JSON removes
structural lines (braces, keys), so the output no longer parses as JSON.
Both behaviors follow from trim-1's documented line-level design — its
card discloses "saves little by design" for *short* tool outputs and
"Line-level only" as a limit. trimx's contribution is a record-level
path for the large structured outputs that design does not cover.

## What trimx does

- **Query expansion (text mode).** Runs trim-1 with the original query.
  When trim-1 returns the input unchanged with its "cut below the
  minimum" fallback, retries once with the query expanded by a small,
  curated English intent lexicon (e.g. `money back` / `returns` /
  `reimbursement` all reach the refund group). Expansion is appended,
  never substituted, and reported on stderr as `query expanded`.
- **Record-level JSON mode.** Detects JSON input, segments it into
  records (array elements; object entries; elements of array-valued
  object entries, so a `{"tool": ..., "results": [...]}` envelope is not
  one monolith), scores each record with trim-1, and keeps whole records.
  Record scores on real tool results are sharply bimodal (measured:
  filler records p99 ≈ 0.009, the relevant record ≈ 0.20–0.27), so the
  selection floor is the named point's floor or 4× the median record
  score, whichever is higher. Output is re-emitted as **valid JSON** with
  explicit markers (`{"_trimx_elided": "2999 records removed by trimx"}`)
  wherever records were dropped.

Same fixtures through trimx:

```
$ trimx --query "can I get my money back" policy.md
trimx: kept 8 lines, removed 354, 20840 -> 568 bytes, query expanded   # 97% cut, refund section kept
$ trimx --query "which order was refunded and why" big.json
trimx: kept 2 records, removed 2999, 394668 -> 238 bytes, query expanded
$ ... | python3 -m json.tool   # parses; envelope keys and the refunded record intact
```

## Install and use

Needs Go 1.23 or newer.

```bash
go install github.com/harshitgavita-07/trimx/cmd/trimx@latest
```

```bash
trimx --query "why does the build fail" build.log > build.trimmed.log
cat tool-result.json | trimx --query "which order was refunded and why"
trimx --query "refund policy" --point aggressive --mode text long-doc.md
```

Flags: `--query` (required), `--point` (`conservative`, `balanced`
default, `aggressive` — trim-1's own operating points), `--mode`
(`auto` default, `text`, `json`).

From Go:

```go
import "github.com/harshitgavita-07/trimx"

res := trimx.Compress(context, query, trimx.Options{}) // auto mode
```

## Tests

```bash
go test ./...
```

End-to-end suite: a paraphrase battery on a long document (recall of the
evidence line plus a minimum cut per query), JSON array / object /
wrapped-envelope fixtures asserting valid-JSON output, record recall and
elision markers, a CI-log regression check, determinism, and a baseline
test that documents the trim-1 behavior trimx compensates for (so a
future trim-1 release that fixes these gaps turns that test into a
signal, not a silent lie).

## Limits

- The lexicon is small, English, and curated by hand. It covers common
  retrieval intents; asks outside it behave exactly like raw trim-1.
- JSON mode scores whole records, so a record that mixes relevant and
  irrelevant data is kept whole (still an exact trim-1 line-score
  decision, one level up).
- The 4×-median record floor is calibrated on the fixtures in this repo
  and the probes above, not on trim-1's benchmark suites; no
  generalization beyond these context shapes is claimed.
- The 238-byte example output keeps the envelope keys and the matching
  record plus an elision marker. Workflows that need neighboring records
  for context should use a lower operating point or a lower floor; the
  marker always says how many records were dropped.
- All numbers are single-run measurements on this repo's fixtures on one
  machine. They are not recall benchmarks and not provider-billing
  measurements.
- Elision markers add a `_trimx_elided` key/element; downstream schemas
  that forbid extra fields need to allow it.
- trim-1's own limits (English, line-level scoring, entity chains) all
  still apply underneath.

## Dependencies and license

trimx imports trim-1 (Apache-2.0) as a Go module dependency; no trim-1
source is copied into this repo. trimx's own license file is pending the
author's decision — all rights reserved until then.
