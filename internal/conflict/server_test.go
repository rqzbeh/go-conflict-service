package conflict

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	NewServer(NewStore()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
}

func TestAPILifecycleExpectedOutputs(t *testing.T) {
	store := NewStore()
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID:           "SUP",
		Title:        "ممنوعیت دسته‌چک",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.",
		CircularType: "supervisory",
		IssueDate:    "1403/01/01",
		Topic:        "دسته‌چک",
	}))
	handler := NewServer(store)

	body := `{"id":"NEW","title":"مجوز دسته‌چک","text":"بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند.","issuer_unit":"واحد تست","circular_type":"internal","issue_date":"1404/01/01","topic":"دسته‌چک"}`
	rec := doJSON(handler, http.MethodPost, "/circulars", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	mustDecode(t, rec, &created)
	if created["circular_id"] != "NEW" || created["clause_count"].(float64) != 1 {
		t.Fatalf("created=%v, want NEW with one clause", created)
	}

	rec = doJSON(handler, http.MethodPost, "/circulars/NEW/analyze", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("analyze status=%d body=%s", rec.Code, rec.Body.String())
	}
	var report Report
	mustDecode(t, rec, &report)
	rel := requireRelationship(t, report, "full_contradiction")
	if rel.WinningClauseID != "SUP#1" {
		t.Fatalf("winner=%q, want SUP#1", rel.WinningClauseID)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/circulars/NEW/clauses/1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("clause status=%d body=%s", rec.Code, rec.Body.String())
	}
	var clause Clause
	mustDecode(t, rec, &clause)
	if clause.ID != "NEW#1" || clause.RulingType != "permission" {
		t.Fatalf("clause=%+v, want NEW#1 permission", clause)
	}
}

func TestAPIRejectsBadInputs(t *testing.T) {
	handler := NewServer(NewStore())
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		code   int
		err    string
	}{
		{"invalid json", http.MethodPost, "/circulars", "{", http.StatusBadRequest, "invalid_json"},
		{"unknown field", http.MethodPost, "/circulars", `{"text":"x","extra":true}`, http.StatusBadRequest, "invalid_json"},
		{"missing text", http.MethodPost, "/circulars", `{"title":"x"}`, http.StatusBadRequest, "missing_text"},
		{"missing circular", http.MethodPost, "/circulars/NOPE/analyze", "", http.StatusNotFound, "circular_not_found"},
		{"missing report", http.MethodGet, "/circulars/NOPE/report", "", http.StatusNotFound, "report_not_found"},
		{"rejected review is unsupported", http.MethodPatch, "/relationships/NOPE", `{"status":"rejected"}`, http.StatusBadRequest, "invalid_status"},
		{"trailing json is rejected", http.MethodPost, "/circulars", `{"text":"x"}{"text":"y"}`, http.StatusBadRequest, "invalid_json"},
		{"invalid issue date is rejected", http.MethodPost, "/circulars", `{"text":"بند 1) متن","title":"عنوان","circular_type":"internal","issue_date":"1404/13/01"}`, http.StatusBadRequest, "invalid_issue_date"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(handler, tc.method, tc.path, tc.body)
			if rec.Code != tc.code {
				t.Fatalf("status=%d body=%s, want %d", rec.Code, rec.Body.String(), tc.code)
			}
			if !strings.Contains(rec.Body.String(), tc.err) {
				t.Fatalf("body=%s, want error %q", rec.Body.String(), tc.err)
			}
		})
	}
}

func TestCreateDoesNotOverwriteExistingCircular(t *testing.T) {
	store := NewStore()
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID:           "DUP",
		Title:        "عنوان اولیه",
		Text:         "بند 1) متن اولیه",
		CircularType: "internal",
		IssueDate:    "1404/01/01",
	}))
	handler := NewServer(store)

	rec := doJSON(handler, http.MethodPost, "/circulars", `{"id":"DUP","title":"عنوان دوم","text":"بند 1) متن دوم","circular_type":"internal","issue_date":"1404/01/02"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409", rec.Code, rec.Body.String())
	}
	got, ok := store.Circular("DUP")
	if !ok || got.Title != "عنوان اولیه" {
		t.Fatalf("existing circular was overwritten: %+v", got)
	}
}

func TestClauseUpdateInvalidatesStaleRelationshipReview(t *testing.T) {
	store := NewStore()
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID:           "A",
		Title:        "ممنوعیت",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.",
		CircularType: "supervisory",
		IssueDate:    "1403/01/01",
		Topic:        "دسته‌چک",
	}))
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID:           "B",
		Title:        "مجوز",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند.",
		CircularType: "internal",
		IssueDate:    "1404/01/01",
		Topic:        "دسته‌چک",
	}))
	if _, err := Analyze(store, "B"); err != nil {
		t.Fatal(err)
	}
	rel := requireRelationship(t, mustReport(t, store, "B"), "full_contradiction")
	if _, err := store.UpdateRelationshipReview(rel.ID, ReviewRequest{Status: "accepted", By: "tester"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateClauseText("B", "1", "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند."); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Relationship(rel.ID); ok {
		t.Fatalf("stale relationship remained after clause update: %s", rel.ID)
	}
}

func mustReport(t *testing.T, store *Store, id string) Report {
	t.Helper()
	report, ok := store.Report(id)
	if !ok {
		t.Fatalf("report %q not found", id)
	}
	return report
}

func TestCircularUpdateRemovesStaleRelationships(t *testing.T) {
	store := NewStore()
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID:           "SUP",
		Title:        "ممنوعیت دسته‌چک",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.",
		CircularType: "supervisory",
		IssueDate:    "1403/01/01",
		Topic:        "دسته‌چک",
	}))
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID:           "NEW",
		Title:        "مجوز دسته‌چک",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند.",
		CircularType: "internal",
		IssueDate:    "1404/01/01",
		Topic:        "دسته‌چک",
	}))
	handler := NewServer(store)

	rec := doJSON(handler, http.MethodPost, "/circulars/NEW/analyze", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("analyze status=%d body=%s", rec.Code, rec.Body.String())
	}
	var report Report
	mustDecode(t, rec, &report)
	requireRelationship(t, report, "full_contradiction")

	body := `{"title":"اصلاح دسته‌چک","text":"بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.","issuer_unit":"واحد تست","circular_type":"internal","issue_date":"1404/01/02","topic":"دسته‌چک"}`
	rec = doJSON(handler, http.MethodPut, "/circulars/NEW", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("update circular status=%d body=%s", rec.Code, rec.Body.String())
	}
	var updated Circular
	mustDecode(t, rec, &updated)
	if updated.ID != "NEW" || updated.Title != "اصلاح دسته‌چک" || updated.Clauses[0].RulingType != "prohibition" {
		t.Fatalf("updated=%+v, want edited NEW prohibition circular", updated)
	}
	if len(store.ListRelationships()) != 0 {
		t.Fatalf("stale relationships remained after circular update: %+v", store.ListRelationships())
	}

	rec = doJSON(handler, http.MethodPut, "/circulars/NOPE", body)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing update status=%d body=%s, want 404", rec.Code, rec.Body.String())
	}
}

func TestSearchClauseUpdateReviewAndPersistence(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "store.json")
	store := NewStore(statePath)
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID:           "SUP",
		Title:        "ممنوعیت دسته‌چک",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.",
		CircularType: "supervisory",
		IssueDate:    "1403/01/01",
		Topic:        "دسته‌چک",
	}))
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID:           "NEW",
		Title:        "مجوز دسته‌چک",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند.",
		CircularType: "internal",
		IssueDate:    "1404/01/01",
		Topic:        "دسته‌چک",
	}))
	handler := NewServer(store)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/search?q="+("%D8%B1%DB%8C%D8%B3%DA%A9+%D8%AF%D8%B3%D8%AA%D9%87+%DA%86%DA%A9"), nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "SUP#1") {
		t.Fatalf("search status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(handler, http.MethodPost, "/circulars/NEW/analyze", "")
	var report Report
	mustDecode(t, rec, &report)
	rel := requireRelationship(t, report, "full_contradiction")

	rec = doJSON(handler, http.MethodPatch, "/relationships/"+rel.ID, `{"status":"accepted","by":"tester","note":"ok"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("review status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(handler, http.MethodPut, "/circulars/NEW/clauses/1", `{"text":"بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند."}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}

	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state not written: %v", err)
	}
	loaded := NewStore(statePath)
	if loaded.CircularCount() != 2 {
		t.Fatalf("loaded circulars=%d, want 2", loaded.CircularCount())
	}
	if storedRel, ok := loaded.Relationship(rel.ID); ok {
		t.Fatalf("stale reviewed relationship survived clause edit: %+v", storedRel)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/circulars/NEW", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if store.CircularCount() != 1 {
		t.Fatalf("circulars after delete=%d, want 1", store.CircularCount())
	}
}

func TestRelationshipsDefaultToActionableItems(t *testing.T) {
	store := NewStore()
	store.SaveReport(Report{
		CircularID: "test",
		Summary:    map[string]int{"total": 2},
		Relationships: []Relationship{
			{
				ID:               "A#1__B#1",
				RelationshipType: "full_contradiction",
				SourceClauseID:   "A#1",
				TargetClauseID:   "B#1",
				Evidence:         map[string]any{"source_text": "مجوز", "target_text": "ممنوعیت"},
			},
			{
				ID:               "A#2__B#2",
				RelationshipType: "overlap_without_conflict",
				SourceClauseID:   "A#2",
				TargetClauseID:   "B#2",
				Evidence:         map[string]any{"source_text": "هم‌پوشانی اول", "target_text": "هم‌پوشانی دوم"},
			},
		},
	})
	handler := NewServer(store)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/relationships", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var actionable struct {
		Count int            `json:"count"`
		Items []Relationship `json:"items"`
	}
	mustDecode(t, rec, &actionable)
	if actionable.Count != 1 || actionable.Items[0].RelationshipType != "full_contradiction" {
		t.Fatalf("actionable=%+v, want only full_contradiction", actionable)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/relationships?actionable=false", nil))
	var all struct {
		Count int            `json:"count"`
		Items []Relationship `json:"items"`
	}
	mustDecode(t, rec, &all)
	if all.Count != 2 {
		t.Fatalf("all count=%d, want 2", all.Count)
	}
}

func TestDeepReviewRelationshipEndpointStoresFallbackReview(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	store := NewStore()
	store.SaveReport(Report{
		CircularID: "test",
		Summary:    map[string]int{"total": 1},
		Relationships: []Relationship{{
			ID:               "A#1__B#1",
			RelationshipType: "full_contradiction",
			SourceClauseID:   "A#1",
			TargetClauseID:   "B#1",
			ResolverStatus:   "resolved",
			WinningClauseID:  "B#1",
			RequiredAction:   "amend_or_reject_losing_clause",
			Rationale:        "یک بند مجوز و بند دیگر ممنوعیت می‌دهد.",
			Evidence:         map[string]any{"source_text": "مجوز دریافت دسته‌چک", "target_text": "ممنوعیت دریافت دسته‌چک"},
		}},
	})
	handler := NewServer(store)

	rec := doJSON(handler, http.MethodPost, "/relationships/A%231__B%231/deep-review", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var rel Relationship
	mustDecode(t, rec, &rel)
	if rel.DeepReview == nil {
		t.Fatal("deep review was not stored")
	}
	if rel.DeepReview.GeneratedByLLM {
		t.Fatal("GeneratedByLLM=true, want deterministic fallback with empty env")
	}
	if rel.DeepReview.Verdict != "conflict" || rel.DeepReview.Severity != "high" {
		t.Fatalf("deep review=%+v, want conflict/high", rel.DeepReview)
	}
	stored, ok := store.Relationship("A#1__B#1")
	if !ok || stored.DeepReview == nil {
		t.Fatalf("stored=%+v ok=%v, want stored deep review", stored, ok)
	}
}

func TestSaveReportPreservesRelationshipReviewState(t *testing.T) {
	store := NewStore()
	rel := Relationship{
		ID:               "A#1__B#1",
		RelationshipType: "full_contradiction",
		SourceClauseID:   "A#1",
		TargetClauseID:   "B#1",
		Evidence:         map[string]any{"source_text": "مجوز", "target_text": "ممنوعیت"},
	}
	store.SaveReport(Report{CircularID: "archive", Relationships: []Relationship{rel}})
	_, err := store.SaveDeepReview(rel.ID, DeepReview{
		Verdict:           "compatible",
		Severity:          "none",
		PlainExplanation:  "مدل اشتباه downgrade کرده است.",
		LegalReason:       "اشتباه",
		RecommendedAction: "اقدامی لازم نیست.",
		GeneratedByLLM:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateRelationshipReview(rel.ID, ReviewRequest{Status: "accepted", By: "tester", Note: "ok"}); err != nil {
		t.Fatal(err)
	}

	fresh := rel
	fresh.Confidence = 0.91
	store.SaveReport(Report{CircularID: "archive", Relationships: []Relationship{fresh}})
	stored, ok := store.Relationship(rel.ID)
	if !ok {
		t.Fatal("relationship not stored")
	}
	if stored.DeepReview == nil || stored.DeepReview.Verdict != "conflict" || stored.DeepReview.GeneratedByLLM {
		t.Fatalf("deep review was not sanitized while preserving state: %+v", stored.DeepReview)
	}
	if stored.ReviewStatus != "accepted" || stored.ReviewedBy != "tester" || stored.ReviewNote != "ok" {
		t.Fatalf("review state was not preserved: %+v", stored)
	}
	if stored.Confidence != 0.91 {
		t.Fatalf("fresh analysis fields were not updated: confidence=%v", stored.Confidence)
	}
}

func TestUpsertCircularOnlyClearsAnalysisWhenContentChanges(t *testing.T) {
	store := NewStore()
	req := CircularRequest{
		ID:           "A",
		Title:        "بخشنامه پایه",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.",
		IssuerUnit:   "واحد آزمون",
		CircularType: "internal",
		IssueDate:    "1404/01/01",
		Topic:        "دسته‌چک",
	}
	store.UpsertCircular(ParseCircular(req))
	store.SaveReport(Report{CircularID: "archive", Relationships: []Relationship{{
		ID:               "A#1__B#1",
		RelationshipType: "full_contradiction",
		SourceClauseID:   "A#1",
		TargetClauseID:   "B#1",
		Evidence:         map[string]any{"source_text": "منع", "target_text": "مجوز"},
	}}})
	if _, err := store.UpdateRelationshipReview("A#1__B#1", ReviewRequest{Status: "accepted", By: "tester"}); err != nil {
		t.Fatal(err)
	}

	store.UpsertCircular(ParseCircular(req))
	rel, ok := store.Relationship("A#1__B#1")
	if !ok || rel.ReviewStatus != "accepted" {
		t.Fatalf("unchanged upsert lost review state: rel=%+v ok=%v", rel, ok)
	}

	req.Text = "بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند."
	store.UpsertCircular(ParseCircular(req))
	if rel, ok := store.Relationship("A#1__B#1"); ok {
		t.Fatalf("changed upsert kept stale relationship: %+v", rel)
	}
}

func doJSON(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func mustDecode(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
}
