# trimx

A companion front-end for [trim-1](https://github.com/caveman-ai/trim-1)
(Caveman's 1.2 MB context-trimming model) that fixes two context shapes
where trim-1 silently does nothing — or breaks the output. trim-1's model
is untouched; trimx sits in front of it and uses its public `Score` /
`Compress` API and its own named operating points.

## The gaps (reproduced against trim-1 v0.1.0, 2026-11-12)

**1. Paraphrased asks silently no-op on prose.** trim-1 scores lines
against the query's own words. On a 20.8 KB policy document:

```
$ trim --query "what is the refund policy" policy.md
trim: kept 8 lines, removed 354, 20840 -> 568 bytes        # works
$ trim --query "can I get my money back" policy.md
trim: unchanged (cut below the minimum)                    # 0% cut, silently
$ trim --query "how do returns work" policy.md
trim: unchanged (cut below the minimum)                    # 0% cut, silently
```

Agents paraphrase constantly, so the tool silently does nothing exactly
when the ask is phrased the way people (and agents) actually phrase it.
The stderr message ("cut below the minimum") also misattributes the cause:
the file is identical; only the query wording changed. trim-1's model card
discloses multi-hop entity chains as a limit; single-hop synonymy like
this is not in the list.

**2. Pretty-printed JSON tool results are invisible to it.** A 395 KB
JSON tool envelope (3,000 order records, one refunded) — the dominant
shape of agent context:

```
$ trim --query "which order was refunded and why" big.json
trim: unchanged (cut below the minimum)        # 0% cut, answer inside
$ trim --query "damaged" big.json | python3 -m json.tool
json.decoder.JSONDecodeError                   # when it does cut, output is invalid JSON
```

Line-level scoring cannot isolate one record among thousands of
identical-looking lines, and a line-level cut of pretty JSON rips out
structural lines (braces, keys), producing output that no longer parses.
(The model card discloses "saves little by design" for *short* tool
outputs; this is large structured output, and invalid-JSON emission is
not disclosed.)

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
  and the probes above, not on trim-1's benchmark suites.
- Elision markers add a `_trimx_elided` key/element; downstream schemas
  that forbid extra fields need to allow it.
- trim-1's own limits (English, line-level scoring, entity chains) all
  still apply underneath.
