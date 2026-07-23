package conflict

import (
	"context"
	"strings"
	"testing"
)

func TestRealOpenAICompatibleEndpoint(t *testing.T) {
	cfg, ok := LLMConfigFromEnv()
	if !ok {
		t.Skip("OPENAI_BASE_URL, OPENAI_API_KEY, and OPENAI_MODEL are required for real LLM integration")
	}
	a := Clause{
		ID:                  "REAL-A#1",
		Subject:             "دسته چک",
		RulingType:          "permission",
		OriginalText:        "بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند.",
		NormalizedText:      NormalizePersian("بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند."),
		ExtractedConditions: map[string]any{"risk": "high"},
	}
	b := Clause{
		ID:                  "REAL-B#1",
		Subject:             "دسته چک",
		RulingType:          "prohibition",
		OriginalText:        "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.",
		NormalizedText:      NormalizePersian("بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند."),
		ExtractedConditions: map[string]any{"risk": "high"},
	}
	fallback := classifyDeterministic(a, b)
	got, err := NewLLMClient(cfg).Compare(context.Background(), a, b, fallback)
	if err != nil {
		t.Fatal(err)
	}
	if !validRelationshipTypes[got.RelationshipType] {
		t.Fatalf("relationship_type=%q is invalid", got.RelationshipType)
	}
	if got.Confidence < 0.55 || got.Confidence > 1 {
		t.Fatalf("confidence=%v, want 0.55..1", got.Confidence)
	}
	if strings.TrimSpace(got.Rationale) == "" {
		t.Fatal("empty rationale")
	}
	if got.RelationshipType != "full_contradiction" {
		t.Fatalf("relationship_type=%q, want full_contradiction for high-risk permission vs prohibition", got.RelationshipType)
	}
}

func TestRealOpenAICompatibleComplianceSummary(t *testing.T) {
	cfg, ok := LLMConfigFromEnv()
	if !ok {
		t.Skip("OPENAI_BASE_URL, OPENAI_API_KEY, and OPENAI_MODEL are required for real LLM integration")
	}
	lines, err := NewLLMClient(cfg).SummarizeCompliance(context.Background(), []Relationship{{
		SourceClauseID:   "REAL-A#1",
		TargetClauseID:   "REAL-B#1",
		RelationshipType: "full_contradiction",
		ResolverStatus:   "resolved",
		WinningClauseID:  "REAL-B#1",
		RequiredAction:   "amend_or_reject_losing_clause",
		Rationale:        "یک بند اجازه دریافت دسته‌چک برای ریسک زیاد می‌دهد و بند دیگر آن را ممنوع می‌کند.",
		Evidence: map[string]any{
			"source_text": "بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند.",
			"target_text": "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("empty compliance summary")
	}
}

func TestRealOpenAICompatibleDeepReview(t *testing.T) {
	cfg, ok := LLMConfigFromEnv()
	if !ok {
		t.Skip("OPENAI_BASE_URL, OPENAI_API_KEY, and OPENAI_MODEL are required for real LLM integration")
	}
	review, err := NewLLMClient(cfg).DeepReviewRelationship(context.Background(), Relationship{
		ID:               "REAL-A#1__REAL-B#1",
		SourceClauseID:   "REAL-A#1",
		TargetClauseID:   "REAL-B#1",
		RelationshipType: "full_contradiction",
		Confidence:       0.82,
		ResolverStatus:   "resolved",
		WinningClauseID:  "REAL-B#1",
		RequiredAction:   "amend_or_reject_losing_clause",
		Rationale:        "یک بند اجازه دریافت دسته‌چک برای ریسک زیاد می‌دهد و بند دیگر آن را ممنوع می‌کند.",
		Evidence: map[string]any{
			"source_text": "بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند.",
			"target_text": "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !review.GeneratedByLLM {
		t.Fatal("GeneratedByLLM=false, want true")
	}
	if review.Verdict != "conflict" && review.Verdict != "partial_conflict" {
		t.Fatalf("verdict=%q, want conflict or partial_conflict", review.Verdict)
	}
	if strings.TrimSpace(review.PlainExplanation) == "" || strings.TrimSpace(review.LegalReason) == "" || strings.TrimSpace(review.RecommendedAction) == "" {
		t.Fatalf("review has empty required text: %+v", review)
	}
}
