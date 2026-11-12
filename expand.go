package trimx

import "strings"

// lexicon is a small curated set of English intent groups covering common
// retrieval asks in agent contexts (documents, logs, tool results). Every
// token of a group expands to every other token. It is deliberately small
// and auditable; it is not a general thesaurus. Matching is on whole
// lowercase query tokens, so "money back" and "returns" both reach the
// refund-intent group.
var lexicon = [][]string{
	{"refund", "refunds", "refunded", "reimburse", "reimbursement", "chargeback", "return", "returns", "returned", "exchange", "money", "cash", "money-back"},
	{"error", "errors", "fail", "fails", "failed", "failure", "exception", "crash", "panic", "fatal", "broken", "bug"},
	{"login", "log-in", "signin", "sign-in", "auth", "authenticate", "authentication", "credential", "credentials"},
	{"price", "pricing", "cost", "costs", "fee", "fees", "charge", "charges", "billing", "invoice", "payment", "payments", "paid"},
	{"delete", "remove", "drop", "destroy", "purge"},
	{"create", "add", "insert", "register", "provision"},
	{"config", "configuration", "settings", "options", "preferences"},
	{"deploy", "deployment", "release", "ship", "rollout"},
	{"user", "users", "account", "accounts", "customer", "customers"},
	{"order", "orders", "purchase", "purchases"},
	{"item", "items", "product", "products", "sku", "stock"},
	{"slow", "latency", "performance", "lag", "speed"},
	{"timeout", "timed-out", "deadline", "hang", "stuck"},
	{"damaged", "defective", "faulty"},
	{"shipping", "delivery", "delivered", "shipment", "dispatch"},
	{"cancel", "cancelled", "cancellation", "abort", "aborted"},
	{"upgrade", "update", "migrate", "migration"},
	{"security", "vulnerability", "exploit", "breach", "leak"},
}

var tokenIndex = func() map[string]int {
	m := make(map[string]int)
	for i, g := range lexicon {
		for _, w := range g {
			m[w] = i
		}
	}
	return m
}()

func tokenize(query string) []string {
	f := func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-')
	}
	parts := strings.FieldsFunc(strings.ToLower(query), f)
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Expand returns the query plus the synonym mates of every matched token,
// or the query unchanged when no token matches the lexicon. The expansion
// is appended, never substituted, so the original wording always stays in
// play for trim-1's scoring.
func Expand(query string) string {
	groups := map[int]bool{}
	seen := map[string]bool{}
	for _, tok := range tokenize(query) {
		seen[tok] = true
		if g, ok := tokenIndex[tok]; ok {
			groups[g] = true
		}
	}
	if len(groups) == 0 {
		return query
	}
	var extra []string
	for g := range groups {
		for _, w := range lexicon[g] {
			if !seen[w] {
				seen[w] = true
				extra = append(extra, w)
			}
		}
	}
	if len(extra) == 0 {
		return query
	}
	return query + " " + strings.Join(extra, " ")
}
