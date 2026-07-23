package conflict

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultEmbeddingModel = "gemini-embedding-001"
const defaultEmbedTimeout = 12 * time.Second

// EmbedClient calls an OpenAI-compatible /v1/embeddings endpoint.
// Used for PDF "Embedding / Semantic Search" with a real neural model when available.
type EmbedClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
	cache   sync.Map // textHash -> []float64
}

type embedRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"` // string or []string
}

type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

var (
	embedInitOnce sync.Once
	globalEmbed   *EmbedClient
)

// EmbedConfigFromEnv reads embedding settings. Enabled when BASE_URL+API_KEY exist
// and OPENAI_EMBEDDING is not explicitly off. Model defaults to gemini-embedding-001
// (chat model ag/* does not support embeddings on this gateway).
func EmbedConfigFromEnv() (baseURL, apiKey, model string, timeout time.Duration, ok bool) {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("OPENAI_EMBEDDING")))
	if v == "0" || v == "false" || v == "no" || v == "off" {
		return "", "", "", 0, false
	}
	baseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")), "/")
	apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	model = strings.TrimSpace(os.Getenv("OPENAI_EMBEDDING_MODEL"))
	if model == "" {
		model = defaultEmbeddingModel
	}
	timeout = defaultEmbedTimeout
	if raw := os.Getenv("OPENAI_EMBEDDING_TIMEOUT_SECONDS"); raw != "" {
		if sec, err := strconv.Atoi(raw); err == nil && sec > 0 {
			timeout = time.Duration(sec) * time.Second
		}
	}
	return baseURL, apiKey, model, timeout, baseURL != "" && apiKey != ""
}

func getEmbedClient() *EmbedClient {
	embedInitOnce.Do(func() {
		base, key, model, timeout, ok := EmbedConfigFromEnv()
		if !ok {
			return
		}
		globalEmbed = &EmbedClient{
			baseURL: base,
			apiKey:  key,
			model:   model,
			http:    &http.Client{Timeout: timeout},
		}
	})
	return globalEmbed
}

// ResetEmbedClientForTests clears the singleton (tests only).
func ResetEmbedClientForTests() {
	globalEmbed = nil
	embedInitOnce = sync.Once{}
}

// EmbeddingRuntimeStatus is safe for /health (no secrets).
func EmbeddingRuntimeStatus() map[string]any {
	base, _, model, _, ok := EmbedConfigFromEnv()
	return map[string]any{
		"enabled": ok,
		"model":   model,
		"base_url": func() string {
			if !ok {
				return ""
			}
			return base
		}(),
		"backend": func() string {
			if ok {
				return "openai_compatible_neural"
			}
			return "local_hashed"
		}(),
	}
}

func (c *EmbedClient) cacheKey(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// EmbedOne returns a neural embedding or error.
func (c *EmbedClient) EmbedOne(ctx context.Context, text string) ([]float64, error) {
	vecs, err := c.EmbedMany(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return nil, fmt.Errorf("empty embedding")
	}
	return vecs[0], nil
}

// EmbedMany batches texts (uses cache; only misses hit the network).
func (c *EmbedClient) EmbedMany(ctx context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	var missIdx []int
	var missText []string
	for i, t := range texts {
		norm := NormalizePersian(t)
		if v, ok := c.cache.Load(c.cacheKey(norm)); ok {
			if vec, ok := v.([]float64); ok && len(vec) > 0 {
				out[i] = vec
				continue
			}
		}
		missIdx = append(missIdx, i)
		missText = append(missText, norm)
	}
	if len(missText) == 0 {
		return out, nil
	}
	// Batch in chunks to keep payloads modest.
	const chunk = 32
	for start := 0; start < len(missText); start += chunk {
		end := start + chunk
		if end > len(missText) {
			end = len(missText)
		}
		part := missText[start:end]
		got, err := c.fetch(ctx, part)
		if err != nil {
			return nil, err
		}
		if len(got) != len(part) {
			return nil, fmt.Errorf("embedding count mismatch: got %d want %d", len(got), len(part))
		}
		for j, vec := range got {
			vec = l2normalize(vec)
			idx := missIdx[start+j]
			out[idx] = vec
			c.cache.Store(c.cacheKey(missText[start+j]), vec)
		}
	}
	return out, nil
}

func (c *EmbedClient) fetch(ctx context.Context, inputs []string) ([][]float64, error) {
	var payload any
	if len(inputs) == 1 {
		payload = embedRequest{Model: c.model, Input: inputs[0]}
	} else {
		payload = embedRequest{Model: c.model, Input: inputs}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var er embedResponse
	if err := json.Unmarshal(raw, &er); err != nil {
		return nil, fmt.Errorf("embed decode: %w body=%s", err, truncate(string(raw), 200))
	}
	if er.Error != nil && er.Error.Message != "" {
		return nil, fmt.Errorf("embed api: %s", er.Error.Message)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embed http %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	// Sort by index if provided
	byIdx := make([][]float64, len(inputs))
	for _, d := range er.Data {
		if d.Index >= 0 && d.Index < len(byIdx) {
			byIdx[d.Index] = d.Embedding
		}
	}
	// If indices missing, fall back to order
	filled := 0
	for i := range byIdx {
		if len(byIdx[i]) > 0 {
			filled++
		}
	}
	if filled != len(inputs) {
		if len(er.Data) != len(inputs) {
			return nil, fmt.Errorf("embed incomplete data")
		}
		out := make([][]float64, len(inputs))
		for i, d := range er.Data {
			out[i] = d.Embedding
		}
		return out, nil
	}
	return byIdx, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// EnrichCircularEmbeddings replaces local vectors with neural ones when the client is configured.
// Safe no-op if embeddings API is off or fails (keeps local vectors).
func EnrichCircularEmbeddings(c *Circular) {
	client := getEmbedClient()
	if client == nil || c == nil || len(c.Clauses) == 0 {
		return
	}
	texts := make([]string, len(c.Clauses))
	for i, cl := range c.Clauses {
		texts[i] = cl.NormalizedText
		if texts[i] == "" {
			texts[i] = cl.OriginalText
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	vecs, err := client.EmbedMany(ctx, texts)
	if err != nil || len(vecs) != len(c.Clauses) {
		return
	}
	for i := range c.Clauses {
		if len(vecs[i]) > 0 {
			c.Clauses[i].Embedding = vecs[i]
		}
	}
}

// needsNeuralUpgrade is true when a clause still has the offline 128-d hashed vector
// (or empty) while a neural client is available.
func needsNeuralUpgrade(cl Clause) bool {
	n := len(cl.Embedding)
	return n == 0 || n == embeddingSize
}

// EnrichStoreEmbeddings upgrades any still-local clause vectors in the store (e.g. after
// loading Postgres state from a previous deploy). No-op offline or on API failure.
func EnrichStoreEmbeddings(store *Store) (upgraded int, err error) {
	client := getEmbedClient()
	if client == nil || store == nil {
		return 0, nil
	}
	var texts []string
	type ref struct {
		circID string
		idx    int
	}
	var refs []ref
	for _, c := range store.ListCirculars() {
		for i, cl := range c.Clauses {
			if !needsNeuralUpgrade(cl) {
				continue
			}
			t := cl.NormalizedText
			if t == "" {
				t = cl.OriginalText
			}
			texts = append(texts, t)
			refs = append(refs, ref{circID: c.ID, idx: i})
		}
	}
	if len(texts) == 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	vecs, err := client.EmbedMany(ctx, texts)
	if err != nil {
		return 0, err
	}
	// Group by circular and rewrite once per circular.
	byCirc := map[string]Circular{}
	for i, r := range refs {
		c, ok := byCirc[r.circID]
		if !ok {
			c, ok = store.Circular(r.circID)
			if !ok {
				continue
			}
		}
		if r.idx >= 0 && r.idx < len(c.Clauses) && len(vecs[i]) > 0 {
			c.Clauses[r.idx].Embedding = vecs[i]
			byCirc[r.circID] = c
			upgraded++
		}
	}
	for _, c := range byCirc {
		store.UpsertCircular(c)
	}
	return upgraded, nil
}
