//go:build liveembed

package conflict

import (
	"os"
	"testing"
)

// Run: RUN_REAL_LLM=1 go test -tags liveembed ./internal/conflict -run TestLiveNeuralEmbedding -v -count=1
func TestLiveNeuralEmbedding(t *testing.T) {
	if os.Getenv("OPENAI_BASE_URL") == "" || os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("need OPENAI_*")
	}
	ResetEmbedClientForTests()
	t.Setenv("OPENAI_EMBEDDING", "1")
	if os.Getenv("OPENAI_EMBEDDING_MODEL") == "" {
		t.Setenv("OPENAI_EMBEDDING_MODEL", "gemini-embedding-001")
	}
	c := ParseCircular(CircularRequest{
		ID:           "LIVE-E",
		Title:        "t",
		Text:         "بند 1) سقف تسهیلات مصرفی حداکثر یک میلیارد ریال است.",
		CircularType: "internal",
		IssueDate:    "1404/01/01",
		Topic:        "تسهیلات",
	})
	if len(c.Clauses) == 0 {
		t.Fatal("no clauses")
	}
	dim := len(c.Clauses[0].Embedding)
	if dim < 256 {
		t.Fatalf("expected neural dim>=256, got %d (local fallback?)", dim)
	}
	st := EmbeddingRuntimeStatus()
	if st["backend"] != "openai_compatible_neural" {
		t.Fatalf("status=%v", st)
	}
	t.Logf("neural dim=%d backend=%v model=%v", dim, st["backend"], st["model"])
}
