package trimx

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	trim "github.com/caveman-ai/trim-1"
)

// policyDoc builds a long terms document with one refund-policy section,
// mirroring the real-world probe shape (generic sections + one answer).
func policyDoc() string {
	var b strings.Builder
	for i := 0; i < 120; i++ {
		if i == 57 {
			b.WriteString("## Refund policy\nCustomers may request a full refund within 30 days of delivery for unused items in original packaging. Digital goods are refundable within 14 days only if not downloaded.\n\n")
		}
		fmt.Fprintf(&b, "## Section %d: General terms\nOur standard terms apply. Delivery windows vary by region and carrier. All marketing claims subject to change. Contact support for questions.\n\n", i+1)
	}
	return b.String()
}

// ordersJSON builds a pretty-printed JSON array of order records with one
// refunded order carrying the answer, mirroring agent tool results.
func ordersJSON(n int) string {
	type order struct {
		ID           string `json:"id"`
		Total        int    `json:"total"`
		Status       string `json:"status"`
		Note         string `json:"note,omitempty"`
		RefundReason string `json:"refund_reason,omitempty"`
	}
	orders := make([]order, n)
	for i := range orders {
		orders[i] = order{ID: fmt.Sprintf("ord-%d", i), Total: 10 + i%900, Status: "shipped", Note: "standard fulfillment per policy"}
	}
	orders[n/2].Status = "refunded"
	orders[n/2].RefundReason = "customer reported damaged item"
	b, _ := json.MarshalIndent(orders, "", "  ")
	return string(b)
}

// buildLog builds a CI log with the failure buried in the middle.
func buildLog() string {
	var b strings.Builder
	for i := 0; i < 1400; i++ {
		fmt.Fprintf(&b, "2026-11-12 04:%02d:%02d.123 INFO  http.access method=GET path=/api/v2/items status=200 ms=%d\n", i%60, i%60, 2+i%400)
	}
	b.WriteString("2026-11-12 04:59:01.456 ERROR pkg/auth/session.go:214: undefined: SessionStore.Migrate (build constraint excluded file session_migrate.go)\n")
	b.WriteString("2026-11-12 04:59:01.457 ERROR FAIL github.com/acme/pkg/auth [build failed]\n")
	for i := 0; i < 1400; i++ {
		fmt.Fprintf(&b, "2026-11-12 05:%02d:%02d.789 INFO  http.access method=POST path=/api/v2/orders status=201 ms=%d\n", i%60, i%60, 2+i%400)
	}
	return b.String()
}

// TestBaselineGap documents the trim-1 behavior trimx fixes: paraphrase
// queries no-op on prose, and literal queries no-op on large JSON. If a
// future trim-1 release fixes these, this test should be updated, not
// deleted: trimx must then add no claim on top.
func TestBaselineGap(t *testing.T) {
	doc := policyDoc()
	lit := trim.Compress(doc, "refund", trim.Options{})
	if lit.Unchanged {
		t.Skip("trim-1 no longer cuts the literal query; revisit baseline")
	}
	para := trim.Compress(doc, "can I get my money back", trim.Options{})
	if !para.Unchanged {
		t.Skip("trim-1 now handles this paraphrase; revisit baseline")
	}
	big := ordersJSON(3000)
	r := trim.Compress(big, "which order was refunded and why", trim.Options{})
	if !r.Unchanged {
		t.Skip("trim-1 now cuts large JSON for this query; revisit baseline")
	}
}

func TestParaphraseBattery(t *testing.T) {
	doc := policyDoc()
	queries := []string{
		"can I get my money back",
		"how do returns work",
		"money back guarantee",
		"reimbursement rules",
		"what is the refund policy",
	}
	for _, q := range queries {
		res := Compress(doc, q, Options{})
		if res.Unchanged {
			t.Errorf("query %q: unchanged, want compression", q)
			continue
		}
		if !strings.Contains(res.Output, "30 days") {
			t.Errorf("query %q: refund line lost", q)
		}
		cut := 1 - float64(len(res.Output))/float64(len(doc))
		if cut < 0.5 {
			t.Errorf("query %q: cut %.1f%%, want >= 50%%", q, cut*100)
		}
	}
}

func TestTextLiteralUnchangedBehavior(t *testing.T) {
	doc := policyDoc()
	res := Compress(doc, "refund", Options{})
	if res.Unchanged || !strings.Contains(res.Output, "30 days") {
		t.Fatalf("literal query regressed: %+v", res)
	}
}

func TestJSONModeValidityAndRecall(t *testing.T) {
	big := ordersJSON(1200)
	queries := []string{
		"which order was refunded and why",
		"show me damaged items",
		"refund_reason",
	}
	for _, q := range queries {
		res := Compress(big, q, Options{})
		if res.Unchanged {
			t.Errorf("query %q: unchanged, want compression", q)
			continue
		}
		var v interface{}
		if err := json.Unmarshal([]byte(res.Output), &v); err != nil {
			t.Errorf("query %q: output is not valid JSON: %v", q, err)
		}
		if !strings.Contains(res.Output, "damaged item") {
			t.Errorf("query %q: refunded record lost", q)
		}
		if !strings.Contains(res.Output, elisionKey) {
			t.Errorf("query %q: no elision marker", q)
		}
		cut := 1 - float64(len(res.Output))/float64(len(big))
		if cut < 0.8 {
			t.Errorf("query %q: cut %.1f%%, want >= 80%%", q, cut*100)
		}
	}
}

func TestJSONObjectMode(t *testing.T) {
	obj := `{"meta": {"tool": "search_orders", "count": 3}, "results": {"a": {"status": "shipped"}, "b": {"status": "refunded", "refund_reason": "damaged item"}, "c": {"status": "shipped"}}}`
	res := Compress(obj, "refund", Options{})
	if res.Unchanged {
		t.Skip("small object below cut minimum is acceptable")
	}
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(res.Output), &v); err != nil {
		t.Fatalf("object output invalid JSON: %v", err)
	}
}

// TestJSONWrappedArray covers the dominant real shape: a tool envelope
// object carrying a large results array, e.g. {"tool": ..., "results":
// [...]}. The array must be segmented per element, not scored as one
// monolith.
func TestJSONWrappedArray(t *testing.T) {
	type order struct {
		ID           string `json:"id"`
		Status       string `json:"status"`
		Note         string `json:"note,omitempty"`
		RefundReason string `json:"refund_reason,omitempty"`
	}
	orders := make([]order, 800)
	for i := range orders {
		orders[i] = order{ID: fmt.Sprintf("ord-%d", i), Status: "shipped", Note: "standard fulfillment per policy"}
	}
	orders[400].Status = "refunded"
	orders[400].RefundReason = "customer reported damaged item"
	inner, _ := json.Marshal(orders)
	envelope := fmt.Sprintf(`{"tool": "search_orders", "count": %d, "results": %s}`, len(orders), inner)

	res := Compress(envelope, "which order was refunded and why", Options{})
	if res.Unchanged {
		t.Fatalf("wrapped array: unchanged (%s), want compression", res.Reason)
	}
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(res.Output), &v); err != nil {
		t.Fatalf("wrapped array: output invalid JSON: %v", err)
	}
	if !strings.Contains(res.Output, "damaged item") {
		t.Fatalf("wrapped array: refunded record lost")
	}
	if !strings.Contains(res.Output, `"tool"`) {
		t.Fatalf("wrapped array: envelope keys lost")
	}
	cut := 1 - float64(len(res.Output))/float64(len(envelope))
	if cut < 0.8 {
		t.Fatalf("wrapped array: cut %.1f%%, want >= 80%%", cut*100)
	}
}

func TestInvalidJSONFallsBackToText(t *testing.T) {
	doc := policyDoc()
	res := Compress(doc, "refund", Options{Mode: ModeJSON})
	if res.Unchanged || !strings.Contains(res.Output, "30 days") {
		t.Fatalf("fallback to text mode failed: %+v", res)
	}
}

func TestLogRecallNoRegression(t *testing.T) {
	log := buildLog()
	res := Compress(log, "why does the build fail", Options{})
	if res.Unchanged {
		t.Fatalf("log compression regressed")
	}
	if !strings.Contains(res.Output, "SessionStore.Migrate") {
		t.Fatalf("error line lost")
	}
	cut := 1 - float64(len(res.Output))/float64(len(log))
	if cut < 0.8 {
		t.Fatalf("cut %.1f%%, want >= 80%%", cut*100)
	}
}

func TestDeterminism(t *testing.T) {
	doc := policyDoc()
	a := Compress(doc, "can I get my money back", Options{})
	b := Compress(doc, "can I get my money back", Options{})
	if a.Output != b.Output {
		t.Fatalf("non-deterministic output")
	}
}

func TestExpand(t *testing.T) {
	cases := map[string]string{
		"can I get my money back": "refund",
		"how do returns work":     "refund",
		"why did the build fail":  "error",
		"xyzzy plugh":             "",
	}
	for q, want := range cases {
		got := Expand(q)
		if want == "" {
			if got != q {
				t.Errorf("Expand(%q) = %q, want unchanged", q, got)
			}
			continue
		}
		if !strings.Contains(got, want) {
			t.Errorf("Expand(%q) = %q, want it to contain %q", q, got, want)
		}
		if !strings.HasPrefix(got, q) {
			t.Errorf("Expand(%q) = %q, must preserve original query as prefix", q, got)
		}
	}
}
