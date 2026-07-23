package conflict

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbedClientBatchAndCache(t *testing.T) {
	ResetEmbedClientForTests()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}
		calls++
		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		// always return 2-dim vectors
		var n int
		switch in := req.Input.(type) {
		case string:
			n = 1
		case []any:
			n = len(in)
		default:
			// json numbers array of strings
			raw, _ := json.Marshal(req.Input)
			var arr []string
			if json.Unmarshal(raw, &arr) == nil {
				n = len(arr)
			} else {
				n = 1
			}
		}
		type item struct {
			Embedding []float64 `json:"embedding"`
			Index     int       `json:"index"`
		}
		data := make([]item, n)
		for i := 0; i < n; i++ {
			data[i] = item{Index: i, Embedding: []float64{float64(i + 1), 0}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	c := &EmbedClient{
		baseURL: srv.URL,
		apiKey:  "k",
		model:   "gemini-embedding-001",
		http:    srv.Client(),
	}
	vecs, err := c.EmbedMany(context.Background(), []string{"الف", "ب"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || len(vecs[0]) != 2 {
		t.Fatalf("vecs=%v", vecs)
	}
	// second call should hit cache only
	_, err = c.EmbedMany(context.Background(), []string{"الف", "ب"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d want 1 (cache)", calls)
	}
}

func TestBuildEmbeddingFallsBackLocal(t *testing.T) {
	ResetEmbedClientForTests()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	vec := BuildEmbedding("سقف تسهیلات مصرفی")
	if len(vec) != embeddingSize {
		t.Fatalf("local dim=%d", len(vec))
	}
}
