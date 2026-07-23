package conflict

import (
	"sort"
	"strings"
)

type SearchResult struct {
	Clause Clause  `json:"clause"`
	Score  float64 `json:"score"`
}

// SearchClauses finds topically related clauses via local embedding + lexical
// overlap (PDF: Embedding/Semantic Search). No external model required.
func SearchClauses(store *Store, query string, limit int) []SearchResult {
	qnorm := NormalizePersian(query)
	qvec := BuildEmbedding(qnorm)
	var out []SearchResult
	for _, c := range store.ListCirculars() {
		for _, cl := range c.Clauses {
			dvec := cl.Embedding
			if len(dvec) == 0 {
				dvec = BuildEmbedding(cl.NormalizedText)
			}
			score := SemanticScore(qnorm, cl.NormalizedText, qvec, dvec)
			if cl.Subject != "" && cl.Subject != "عمومی" && strings.Contains(qnorm, cl.Subject) {
				score += 0.12
			}
			if score >= 0.18 {
				out = append(out, SearchResult{Clause: cl, Score: score})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}
