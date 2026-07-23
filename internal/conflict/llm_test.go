package conflict

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLLMCompareUsesOpenAICompatibleEndpoint(t *testing.T) {
	var seenModel, seenAuth string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path=%s, want /chat/completions", r.URL.Path)
		}
		seenAuth = r.Header.Get("Authorization")
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		seenModel = req.Model
		if !strings.Contains(req.Messages[len(req.Messages)-1].Content, "BX-A#1") {
			t.Fatalf("prompt did not include source clause id: %q", req.Messages[len(req.Messages)-1].Content)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"relationship_type\":\"partial_contradiction\",\"confidence\":0.88,\"rationale\":\"دوره سابقه متفاوت است.\"}"}}]}`))
	}))
	defer fake.Close()

	t.Setenv("OPENAI_BASE_URL", fake.URL)
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_MODEL", "ag/gemini-3.6-flash-high")

	a := Clause{ID: "BX-A#1", Subject: "دسته چک", RulingType: "permission", OriginalText: "بند 1) حداقل ۶ ماه سابقه لازم است.", NormalizedText: NormalizePersian("حداقل 6 ماه سابقه لازم است."), ExtractedConditions: map[string]any{"months": 6}}
	b := Clause{ID: "BX-B#1", Subject: "دسته چک", RulingType: "permission", OriginalText: "بند 1) حداقل ۳ ماه سابقه لازم است.", NormalizedText: NormalizePersian("حداقل 3 ماه سابقه لازم است."), ExtractedConditions: map[string]any{"months": 3}}

	// classify() is deterministic by default (LLM_CLASSIFY off) so Analyze stays fast.
	// This test covers the OpenAI-compatible Compare path used by deep-review / opt-in classify.
	fallback := classifyDeterministic(a, b)
	got, err := NewLLMClient(LLMConfig{BaseURL: fake.URL, APIKey: "test-key", Model: "ag/gemini-3.6-flash-high", Timeout: defaultLLMTimeout}).Compare(context.Background(), a, b, fallback)
	if err != nil {
		t.Fatal(err)
	}
	chosen := chooseClassification(fallback, got)
	if chosen.RelationshipType != "partial_contradiction" || chosen.Confidence != 0.88 || !strings.Contains(chosen.Rationale, "LLM:") {
		t.Fatalf("classification=(%s,%v,%s), want LLM partial contradiction", chosen.RelationshipType, chosen.Confidence, chosen.Rationale)
	}
	// default classify must not hit network
	typ, conf, _ := classify(a, b)
	if typ != "partial_contradiction" || conf == 0.88 {
		t.Fatalf("default classify should stay deterministic, got %s conf=%v", typ, conf)
	}
	if seenAuth != "Bearer test-key" {
		t.Fatalf("Authorization=%q, want bearer key", seenAuth)
	}
	if seenModel != "ag/gemini-3.6-flash-high" {
		t.Fatalf("model=%q, want ag/gemini-3.6-flash-high", seenModel)
	}
}

func TestLLMSummarizeComplianceUsesOpenAICompatibleEndpoint(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path=%s, want /chat/completions", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"items\":[\"این بخشنامه با محدودیت ریسک زیاد مغایر است؛ بند نظارتی باید مبنا قرار گیرد.\"]}"}}]}`))
	}))
	defer fake.Close()

	client := NewLLMClient(LLMConfig{BaseURL: fake.URL, APIKey: "test-key", Model: "ag/gemini-3.6-flash-high", Timeout: defaultLLMTimeout})
	lines, err := client.SummarizeCompliance(context.Background(), []Relationship{{
		SourceClauseID:   "A#1",
		TargetClauseID:   "B#1",
		RelationshipType: "full_contradiction",
		ResolverStatus:   "resolved",
		WinningClauseID:  "B#1",
		RequiredAction:   "amend_or_reject_losing_clause",
		Rationale:        "ناسازگار است.",
		Evidence:         map[string]any{"source_text": "مجوز", "target_text": "ممنوعیت"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "مغایر") {
		t.Fatalf("lines=%v", lines)
	}
}

func TestLLMDeepReviewUsesOpenAICompatibleEndpoint(t *testing.T) {
	var seenPrompt string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path=%s, want /chat/completions", r.URL.Path)
		}
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		seenPrompt = req.Messages[len(req.Messages)-1].Content
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"verdict\":\"conflict\",\"severity\":\"high\",\"plain_explanation\":\"یک بند اجازه می‌دهد و بند دیگر همان کار را ممنوع می‌کند.\",\"legal_reason\":\"موضوع و دامنه ریسک در هر دو بند یکسان است اما حکم‌ها متضاد هستند.\",\"recommended_action\":\"بند بازنده اصلاح یا رد شود.\",\"questions\":[\"\",\"آیا بند بالادستی هنوز نافذ است؟\"]}"}}]}`))
	}))
	defer fake.Close()

	client := NewLLMClient(LLMConfig{BaseURL: fake.URL, APIKey: "test-key", Model: "ag/gemini-3.6-flash-high", Timeout: defaultLLMTimeout})
	review, err := client.DeepReviewRelationship(context.Background(), Relationship{
		ID:               "A#1__B#1",
		RelationshipType: "full_contradiction",
		Confidence:       0.82,
		ResolverStatus:   "resolved",
		WinningClauseID:  "B#1",
		RequiredAction:   "amend_or_reject_losing_clause",
		Rationale:        "ناسازگار است.",
		Evidence:         map[string]any{"source_text": "مجاز است", "target_text": "ممنوع است"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seenPrompt, "A#1__B#1") {
		t.Fatalf("prompt=%q, want relationship id", seenPrompt)
	}
	if !review.GeneratedByLLM || review.Verdict != "conflict" || review.Severity != "high" {
		t.Fatalf("review=%+v, want generated conflict/high", review)
	}
	if len(review.Questions) != 1 {
		t.Fatalf("questions=%v, want empty question removed", review.Questions)
	}
}

func TestLLMGuardrailKeepsDeterministicContradiction(t *testing.T) {
	fallback := llmResult{RelationshipType: "full_contradiction", Confidence: 0.82, Rationale: "قاعده قطعی"}
	got := llmResult{RelationshipType: "neutral", Confidence: 0.99, Rationale: "مدل اشتباه گفت خنثی است"}
	chosen := chooseClassification(fallback, got)
	if chosen != fallback {
		t.Fatalf("chosen=%+v, want fallback=%+v", chosen, fallback)
	}
}

func TestParseLLMResultAcceptsJSONFence(t *testing.T) {
	got, err := parseLLMResult("```json\n{\"relationship_type\":\"neutral\",\"confidence\":0.7,\"rationale\":\"ارتباطی نیست.\"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if got.RelationshipType != "neutral" || got.Confidence != 0.7 {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseDeepReviewRequiresUsefulFields(t *testing.T) {
	_, err := parseDeepReview(`{"verdict":"compatible","severity":"none","plain_explanation":"هم‌پوشانی دارد.","legal_reason":"","recommended_action":"اقدامی لازم نیست."}`)
	if err == nil {
		t.Fatal("parseDeepReview succeeded without legal_reason")
	}

	got, err := parseDeepReview("```json\n{\"verdict\":\"compatible\",\"severity\":\"none\",\"plain_explanation\":\"هر دو بند یک حکم مشترک را بیان می‌کنند.\",\"legal_reason\":\"حکم متضاد، سقف متفاوت، یا استثنای ناسازگار دیده نمی‌شود.\",\"recommended_action\":\"بدون اقدام اصلاحی، فقط ثبت به عنوان هم‌پوشانی.\",\"questions\":[\" \",\"آیا نسخه جدیدتری وجود دارد؟\"]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != "compatible" || got.Severity != "none" || len(got.Questions) != 1 {
		t.Fatalf("got=%+v", got)
	}
}

func TestDeepReviewGuardrailKeepsActionableRelationship(t *testing.T) {
	rel := Relationship{RelationshipType: "partial_contradiction"}
	fallback := deterministicDeepReview(rel)
	got := DeepReview{
		Verdict:           "compatible",
		Severity:          "none",
		PlainExplanation:  "مدل اشتباه downgrade کرد.",
		LegalReason:       "اشتباه",
		RecommendedAction: "اقدامی لازم نیست.",
		GeneratedByLLM:    true,
	}
	chosen := chooseDeepReview(rel, got, fallback)
	if chosen.Verdict != "partial_conflict" || chosen.Severity != "medium" || chosen.GeneratedByLLM {
		t.Fatalf("chosen=%+v, want deterministic partial conflict fallback", chosen)
	}
}

func TestDeepReviewGuardrailEscalatesCompatibleRelationship(t *testing.T) {
	rel := Relationship{RelationshipType: "overlap_without_conflict"}
	fallback := deterministicDeepReview(rel)
	got := DeepReview{
		Verdict:           "conflict",
		Severity:          "none",
		PlainExplanation:  "مدل ریسک تازه‌ای دیده است.",
		LegalReason:       "نیازمند بررسی",
		RecommendedAction: "بررسی انسانی.",
		GeneratedByLLM:    true,
	}
	chosen := chooseDeepReview(rel, got, fallback)
	if chosen.Verdict != "needs_human_review" || chosen.Severity != "low" || !chosen.GeneratedByLLM {
		t.Fatalf("chosen=%+v, want LLM escalation converted to human review", chosen)
	}
}

func TestDecodeSSEChatContent(t *testing.T) {
	raw := `data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"choices":[{"delta":{"content":"{\"relationship_type\":\"full_contradiction\","},"finish_reason":null}]}

data: {"choices":[{"delta":{"content":"\"confidence\":0.91,\"rationale\":\"ناسازگار است.\"}"},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`
	content, err := decodeSSEChatContent(raw)
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseLLMResult(content)
	if err != nil {
		t.Fatal(err)
	}
	if got.RelationshipType != "full_contradiction" || got.Confidence != 0.91 {
		t.Fatalf("got=%+v", got)
	}
}

func TestDeterministicComplianceSummaryFallback(t *testing.T) {
	lines, byLLM := SummarizeForCompliance([]Relationship{{
		SourceClauseID:   "A#1",
		TargetClauseID:   "B#1",
		RelationshipType: "partial_contradiction",
		WinningClauseID:  "B#1",
		RequiredAction:   "review",
	}})
	if byLLM {
		t.Fatal("byLLM=true, want false without env")
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "partial_contradiction") {
		t.Fatalf("lines=%v", lines)
	}
}
