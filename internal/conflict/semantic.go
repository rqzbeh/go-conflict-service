package conflict

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
)

const embeddingSize = 128

// synonym groups expand query/document tokens for lightweight semantic retrieval
// without an external embedding service (PDF: Embedding/Semantic Search).
var synonymGroups = [][]string{
	{"دسته چک", "دستهچک", "چک"},
	{"کارت اعتباری", "کارت", "کارت خرید اقساطی"},
	{"تسهیلات", "وام", "وام مصرفی", "اعتبار", "سقف تسهیلات"},
	{"ابزارهای اعتباری", "ابزار اعتباری", "محصول اعتباری", "محصولات اعتباری", "خدمات اعتباری"},
	{"قرض الحسنه", "قرض‌الحسنه", "قرض"},
	{"سپرده", "حساب", "حساب فعال"},
	{"ریسک", "rbci", "خطر", "اعتبارسنجی"},
	{"محروم", "ممنوع", "نباید"},
	{"مجاز", "قابل ارائه", "امکان پذیر"},
	{"لغو", "نسخ", "جایگزین", "اصلاح", "باطل"},
	{"سابقه", "مدت", "ماه"},
	{"ضامن", "وثیقه", "تضمین"},
	{"سقف", "حداکثر", "مبلغ"},
}

var semanticAliases map[string][]string

func init() {
	semanticAliases = map[string][]string{}
	for _, group := range synonymGroups {
		normed := make([]string, 0, len(group))
		for _, g := range group {
			normed = append(normed, NormalizePersian(g))
		}
		for _, g := range normed {
			for _, other := range normed {
				if g == other {
					continue
				}
				semanticAliases[g] = appendUnique(semanticAliases[g], other)
				parts := strings.Fields(g)
				if len(parts) > 0 {
					semanticAliases[parts[0]] = appendUnique(semanticAliases[parts[0]], other)
				}
			}
		}
	}
}

func appendUnique(xs []string, v string) []string {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}

// BuildEmbedding returns a semantic vector for text.
//
// Prefer neural embeddings from the OpenAI-compatible endpoint
// (OPENAI_EMBEDDING_MODEL, default gemini-embedding-001) when configured;
// fall back to local hashed bag-of-tokens + synonym expansion offline.
func BuildEmbedding(text string) []float64 {
	if c := getEmbedClient(); c != nil {
		ctx, cancel := context.WithTimeout(context.Background(), defaultEmbedTimeout)
		defer cancel()
		if vec, err := c.EmbedOne(ctx, text); err == nil && len(vec) > 0 {
			return vec
		}
	}
	return buildLocalEmbedding(text)
}

// buildLocalEmbedding is the offline deterministic fallback (hashed + synonyms).
func buildLocalEmbedding(text string) []float64 {
	vec := make([]float64, embeddingSize)
	for token, w := range weightedTokens(text) {
		addToken(vec, token, w)
		for _, alias := range semanticAliases[token] {
			addToken(vec, alias, w*0.55)
		}
	}
	for _, g := range charNgrams(NormalizePersian(text), 3) {
		addToken(vec, "c3:"+g, 0.25)
	}
	return l2normalize(vec)
}

func weightedTokens(text string) map[string]float64 {
	out := map[string]float64{}
	norm := NormalizePersian(text)
	for tok := range tokens(norm) {
		out[tok] += 1
	}
	for _, phrase := range []string{
		"دسته چک", "کارت اعتباری", "وام مصرفی", "قرض الحسنه",
		"ابزارهای اعتباری", "سقف تسهیلات", "حساب فعال",
	} {
		p := NormalizePersian(phrase)
		if strings.Contains(norm, p) {
			out[p] += 2
		}
	}
	return out
}

func charNgrams(s string, n int) []string {
	rs := []rune(strings.ReplaceAll(s, " ", ""))
	if len(rs) < n {
		return nil
	}
	out := make([]string, 0, len(rs)-n+1)
	seen := map[string]bool{}
	for i := 0; i+n <= len(rs); i++ {
		g := string(rs[i : i+n])
		if seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
	}
	return out
}

func addToken(vec []float64, token string, weight float64) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(token)))
	vec[int(h.Sum32())%len(vec)] += weight
	h2 := fnv.New32a()
	_, _ = h2.Write([]byte("salt:" + strings.ToLower(token)))
	vec[int(h2.Sum32())%len(vec)] += weight * 0.5
}

func l2normalize(vec []float64) []float64 {
	norm := 0.0
	for _, v := range vec {
		norm += v * v
	}
	if norm == 0 {
		return vec
	}
	norm = math.Sqrt(norm)
	for i := range vec {
		vec[i] /= norm
	}
	return vec
}

func embeddingScore(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	score := 0.0
	for i := range a {
		score += a[i] * b[i]
	}
	return score
}

// SemanticScore combines embedding cosine with lexical overlap for retrieval ranking.
func SemanticScore(query, doc string, qvec, dvec []float64) float64 {
	emb := embeddingScore(qvec, dvec)
	lex := overlapScore(NormalizePersian(query), NormalizePersian(doc))
	return 0.65*emb + 0.35*lex
}
