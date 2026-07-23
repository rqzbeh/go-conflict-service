package conflict

import (
	"strings"
	"testing"
)

func TestAnalyzeFindsThresholdConflictAndNewerWinner(t *testing.T) {
	store := NewStore()
	old := ParseCircular(CircularRequest{
		ID:           "OLD",
		Title:        "شرایط دسته‌چک",
		Text:         "بند 1) دسته‌چک به مشتریانی قابل ارائه است که حداقل ۶ ماه از تاریخ افتتاح حساب گذشته باشد.",
		CircularType: "internal",
		IssueDate:    "1403/01/01",
		Topic:        "دسته‌چک",
	})
	newer := ParseCircular(CircularRequest{
		ID:           "NEW",
		Title:        "اصلاح شرایط دسته‌چک",
		Text:         "بند 1) دسته‌چک به مشتریانی قابل ارائه است که حداقل ۳ ماه از تاریخ افتتاح حساب گذشته باشد.",
		CircularType: "internal",
		IssueDate:    "1404/01/01",
		Topic:        "دسته‌چک",
	})
	store.UpsertCircular(old)
	store.UpsertCircular(newer)

	report, err := Analyze(store, "NEW")
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary["partial_contradiction"] == 0 {
		t.Fatalf("summary=%v, want partial contradiction", report.Summary)
	}
	if got := report.Relationships[0].WinningClauseID; got != "NEW#1" {
		t.Fatalf("winner=%q, want NEW#1", got)
	}
}

func TestSupervisoryCircularWinsOverInternal(t *testing.T) {
	store := NewStore()
	internal := ParseCircular(CircularRequest{
		ID:           "INT",
		Title:        "مجوز دسته‌چک",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند.",
		CircularType: "internal",
		IssueDate:    "1404/01/01",
		Topic:        "دسته‌چک",
	})
	supervisory := ParseCircular(CircularRequest{
		ID:           "SUP",
		Title:        "محدودیت ریسک",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم می‌باشند.",
		CircularType: "supervisory",
		IssueDate:    "1403/01/01",
		Topic:        "دسته‌چک",
	})
	store.UpsertCircular(internal)
	store.UpsertCircular(supervisory)

	report, err := Analyze(store, "INT")
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary["full_contradiction"] == 0 {
		t.Fatalf("summary=%v, want full contradiction", report.Summary)
	}
	if got := report.Relationships[0].WinningClauseID; got != "SUP#1" {
		t.Fatalf("winner=%q, want SUP#1", got)
	}
}

func TestGeneralProductPermissionDoesNotConflictWithRiskRestriction(t *testing.T) {
	a := buildClause(Circular{ID: "A"}, "1", "بند 1) دسته‌چک به مشتریان دارای ۶ ماه سابقه قابل ارائه است.")
	b := buildClause(Circular{ID: "B"}, "1", "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.")

	typ, _, _ := classify(a, b)
	if typ == "full_contradiction" {
		t.Fatalf("classification=%s, want non-contradiction because only one clause is risk-scoped", typ)
	}
}

func TestExplicitSupersessionByCircularReference(t *testing.T) {
	store := NewStore()
	old := ParseCircular(CircularRequest{
		ID:           "BX-2000",
		Title:        "شرایط دسته‌چک",
		Text:         "بند 1) دسته‌چک به مشتریانی قابل ارائه است که حداقل ۶ ماه سابقه داشته باشند.",
		CircularType: "internal",
		IssueDate:    "1403/01/01",
		Topic:        "دسته‌چک",
	})
	newer := ParseCircular(CircularRequest{
		ID:           "BX-2001",
		Title:        "اصلاح بخشنامه BX-2000",
		Text:         "بند 1) بند 1 بخشنامه BX-2000 لغو و با شرط حداقل ۳ ماه سابقه جایگزین می‌شود.",
		CircularType: "internal",
		IssueDate:    "1404/01/01",
		Topic:        "دسته‌چک",
	})
	store.UpsertCircular(old)
	store.UpsertCircular(newer)

	report, err := Analyze(store, "BX-2001")
	if err != nil {
		t.Fatal(err)
	}
	rel := requireRelationship(t, report, "supersession")
	if rel.WinningClauseID != "BX-2001#1" {
		t.Fatalf("winner=%q, want BX-2001#1", rel.WinningClauseID)
	}
	if rel.RequiredAction != "mark_superseded" {
		t.Fatalf("action=%q, want mark_superseded", rel.RequiredAction)
	}
}

func TestSameLevelSameDateConflictNeedsReview(t *testing.T) {
	a := ParseCircular(CircularRequest{
		ID:           "A",
		Title:        "مجوز دسته‌چک",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند.",
		CircularType: "internal",
		IssueDate:    "1404/01/01",
		Topic:        "دسته‌چک",
	})
	b := ParseCircular(CircularRequest{
		ID:           "B",
		Title:        "ممنوعیت دسته‌چک",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.",
		CircularType: "internal",
		IssueDate:    "1404/01/01",
		Topic:        "دسته‌چک",
	})
	store := NewStore()
	store.UpsertCircular(a)
	store.UpsertCircular(b)

	report, err := Analyze(store, "A")
	if err != nil {
		t.Fatal(err)
	}
	rel := requireRelationship(t, report, "full_contradiction")
	if rel.ResolverStatus != "needs_review" {
		t.Fatalf("resolver=%q, want needs_review", rel.ResolverStatus)
	}
}

func TestNonPaddedJalaliDatesUseCalendarOrder(t *testing.T) {
	store := NewStore()
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID:           "OLD",
		Title:        "شرایط دسته‌چک",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند.",
		CircularType: "internal",
		IssueDate:    "1404/9/10",
		Topic:        "دسته‌چک",
	}))
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID:           "NEW",
		Title:        "اصلاح شرایط دسته‌چک",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.",
		CircularType: "internal",
		IssueDate:    "1404/10/01",
		Topic:        "دسته‌چک",
	}))

	report, err := Analyze(store, "NEW")
	if err != nil {
		t.Fatal(err)
	}
	rel := requireRelationship(t, report, "full_contradiction")
	if rel.WinningClauseID != "NEW#1" {
		t.Fatalf("winner=%q, want NEW#1 for 1404/10/01 > 1404/9/10", rel.WinningClauseID)
	}
}

func TestArchiveScanFindsHiddenOldConflict(t *testing.T) {
	store := NewStore()
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID:           "OLD-A",
		Title:        "مجوز دسته‌چک",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند.",
		CircularType: "internal",
		IssueDate:    "1402/01/01",
		Topic:        "دسته‌چک",
	}))
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID:           "OLD-B",
		Title:        "ممنوعیت دسته‌چک",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.",
		CircularType: "internal",
		IssueDate:    "1403/01/01",
		Topic:        "دسته‌چک",
	}))

	report := ScanArchive(store)
	rel := requireRelationship(t, report, "full_contradiction")
	if rel.WinningClauseID != "OLD-B#1" {
		t.Fatalf("winner=%q, want OLD-B#1", rel.WinningClauseID)
	}
}

func TestArchiveScanDoesNotCallLLMPerClause(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("OPENAI_API_KEY", "test")
	t.Setenv("OPENAI_MODEL", "ag/gemini-3.6-flash-high")
	store := NewStore()
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID:           "A",
		Title:        "مجوز دسته‌چک",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند.",
		CircularType: "internal",
		IssueDate:    "1402/01/01",
		Topic:        "دسته‌چک",
	}))
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID:           "B",
		Title:        "ممنوعیت دسته‌چک",
		Text:         "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.",
		CircularType: "internal",
		IssueDate:    "1403/01/01",
		Topic:        "دسته‌چک",
	}))

	report := ScanArchive(store)
	rel := requireRelationship(t, report, "full_contradiction")
	if strings.Contains(rel.Rationale, "LLM") {
		t.Fatalf("archive scan used LLM rationale: %q", rel.Rationale)
	}
}

func requireRelationship(t *testing.T, report Report, typ string) Relationship {
	t.Helper()
	for _, rel := range report.Relationships {
		if rel.RelationshipType == typ {
			return rel
		}
	}
	t.Fatalf("report summary=%v, want relationship type %q", report.Summary, typ)
	return Relationship{}
}
