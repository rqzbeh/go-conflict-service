package conflict

import (
	"strings"
	"testing"
)

func TestExtractClausesAndConditions(t *testing.T) {
	c := ParseCircular(CircularRequest{
		ID:           "T-1",
		Title:        "تست",
		Text:         "بند 1) حداقل ۶ ماه سابقه لازم است.\nبند 2) سقف ۵۰,۰۰۰,۰۰۰ تومان است.",
		CircularType: "internal",
	})
	if len(c.Clauses) != 2 {
		t.Fatalf("clauses=%d, want 2", len(c.Clauses))
	}
	if got := c.Clauses[0].ExtractedConditions["months"]; got != 6 {
		t.Fatalf("months=%v, want 6", got)
	}
	if got := c.Clauses[1].ExtractedConditions["amount_toman"]; got != 50000000 {
		t.Fatalf("amount=%v, want 50000000", got)
	}
}

func TestAmountExtractionRialMillionAndWords(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"بند 1) سقف تسهیلات مصرفی حداکثر ۵۰۰ میلیون ریال است.", 50_000_000},
		{"بند 1) سقف تسهیلات مصرفی حداکثر یک میلیارد ریال است.", 100_000_000},
		{"بند 1) سقف تسهیلات حداکثر پانصد میلیون ریال تعیین می‌شود.", 50_000_000},
		{"بند 1) سقف کارت ۸۰ میلیون تومان است.", 80_000_000},
		{"بند 1) سقف 1000000000 ریال است.", 100_000_000},
	}
	for _, tc := range cases {
		c := ParseCircular(CircularRequest{ID: "A", Title: "تسهیلات", Text: tc.text, CircularType: "internal"})
		if len(c.Clauses) != 1 {
			t.Fatalf("clauses=%d text=%q", len(c.Clauses), tc.text)
		}
		got, _ := asInt(c.Clauses[0].ExtractedConditions["amount_toman"])
		if got != tc.want {
			t.Fatalf("amount=%d want=%d text=%q subject=%s ruling=%s", got, tc.want, tc.text, c.Clauses[0].Subject, c.Clauses[0].RulingType)
		}
		if c.Clauses[0].Subject == "عمومی" {
			t.Fatalf("subject stayed عمومی for %q", tc.text)
		}
	}
}

func TestPDFCeilingConflictAndResolver(t *testing.T) {
	store := NewStore()
	oldC := ParseCircular(CircularRequest{
		ID: "PDF-OLD-CEIL", Title: "سقف قدیمی", CircularType: "internal", IssueDate: "1403/01/01", Topic: "تسهیلات",
		Text: "بند 1) سقف تسهیلات مصرفی برای مشتریان عادی حداکثر پانصد میلیون ریال است.",
	})
	newC := ParseCircular(CircularRequest{
		ID: "PDF-NEW-CEIL", Title: "سقف جدید", CircularType: "internal", IssueDate: "1404/01/01", Topic: "تسهیلات",
		Text: "بند 1) سقف تسهیلات مصرفی برای مشتریان عادی حداکثر یک میلیارد ریال است.",
	})
	store.UpsertCircular(oldC)
	store.UpsertCircular(newC)
	report, err := Analyze(store, "PDF-NEW-CEIL")
	if err != nil {
		t.Fatal(err)
	}
	var hit *Relationship
	for i := range report.Relationships {
		r := &report.Relationships[i]
		if (r.SourceClauseID == "PDF-NEW-CEIL#1" && r.TargetClauseID == "PDF-OLD-CEIL#1") ||
			(r.SourceClauseID == "PDF-OLD-CEIL#1" && r.TargetClauseID == "PDF-NEW-CEIL#1") {
			hit = r
			break
		}
	}
	if hit == nil {
		t.Fatalf("no relationship between ceiling circulars; summary=%v rels=%+v", report.Summary, report.Relationships)
	}
	if hit.RelationshipType != "partial_contradiction" {
		t.Fatalf("type=%s want partial_contradiction rationale=%s", hit.RelationshipType, hit.Rationale)
	}
	if hit.WinningClauseID != "PDF-NEW-CEIL#1" {
		t.Fatalf("winner=%s want PDF-NEW-CEIL#1", hit.WinningClauseID)
	}
}

func TestPDFSupervisoryBeatsInternal(t *testing.T) {
	store := NewStore()
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID: "PDF-SUP", Title: "بالادستی", CircularType: "supervisory", IssueDate: "1403/01/01", Topic: "دسته‌چک",
		Text: "بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.",
	}))
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID: "PDF-INT", Title: "داخلی", CircularType: "internal", IssueDate: "1404/06/01", Topic: "دسته‌چک",
		Text: "بند 1) مشتریان با سطح ریسک زیاد مجاز به دریافت دسته‌چک هستند.",
	}))
	report, err := Analyze(store, "PDF-INT")
	if err != nil {
		t.Fatal(err)
	}
	var hit *Relationship
	for i := range report.Relationships {
		r := &report.Relationships[i]
		if stringsContainsEither(r.SourceClauseID, r.TargetClauseID, "PDF-SUP#1", "PDF-INT#1") {
			hit = r
			break
		}
	}
	if hit == nil {
		t.Fatalf("missing sup vs int relation: %+v", report.Relationships)
	}
	if hit.RelationshipType != "full_contradiction" {
		t.Fatalf("type=%s want full_contradiction", hit.RelationshipType)
	}
	if hit.WinningClauseID != "PDF-SUP#1" {
		t.Fatalf("winner=%s want PDF-SUP#1", hit.WinningClauseID)
	}
}

func TestPDFSameDateNeedsReview(t *testing.T) {
	store := NewStore()
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID: "PDF-A", Title: "واحد اعتبار", CircularType: "internal", IssueDate: "1403/05/01", IssuerUnit: "اعتبارات", Topic: "دسته‌چک",
		Text: "بند 1) برای صدور دسته‌چک حداقل ۶ ماه سابقه حساب الزامی است.",
	}))
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID: "PDF-B", Title: "واحد حقوقی", CircularType: "internal", IssueDate: "1403/05/01", IssuerUnit: "حقوقی", Topic: "دسته‌چک",
		Text: "بند 1) برای صدور دسته‌چک حداقل ۳ ماه سابقه حساب الزامی است.",
	}))
	report, err := Analyze(store, "PDF-B")
	if err != nil {
		t.Fatal(err)
	}
	var hit *Relationship
	for i := range report.Relationships {
		r := &report.Relationships[i]
		if stringsContainsEither(r.SourceClauseID, r.TargetClauseID, "PDF-A#1", "PDF-B#1") {
			hit = r
			break
		}
	}
	if hit == nil {
		t.Fatalf("missing same-date relation: %+v", report.Relationships)
	}
	if hit.ResolverStatus != "needs_review" {
		t.Fatalf("resolver=%s want needs_review type=%s", hit.ResolverStatus, hit.RelationshipType)
	}
}

func TestPDFExplicitSupersession(t *testing.T) {
	store := NewStore()
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID: "PDF-BASE", Title: "پایه", CircularType: "internal", IssueDate: "1403/01/01", Topic: "کارت",
		Text: "بند 1) کارت اعتباری با حداقل درآمد ۲۰ میلیون قابل ارائه است.\nبند 2) سقف کارت ۵۰ میلیون تومان است.",
	}))
	store.UpsertCircular(ParseCircular(CircularRequest{
		ID: "PDF-PART", Title: "اصلاح", CircularType: "internal", IssueDate: "1404/01/01", Topic: "کارت",
		Text: "بند 1) بند 2 بخشنامه PDF-BASE اصلاح می‌شود و سقف کارت به ۸۰ میلیون تومان افزایش می‌یابد.",
	}))
	report, err := Analyze(store, "PDF-PART")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range report.Relationships {
		if r.RelationshipType == "supersession" && (r.TargetClauseID == "PDF-BASE#2" || r.TargetClauseID == "PDF-BASE#1" || r.SourceClauseID == "PDF-BASE#2") {
			found = true
			if r.WinningClauseID != "PDF-PART#1" {
				t.Fatalf("winner=%s want PDF-PART#1", r.WinningClauseID)
			}
		}
	}
	if !found {
		t.Fatalf("expected supersession to PDF-BASE, got %+v", report.Relationships)
	}
}

func stringsContainsEither(a, b, x, y string) bool {
	pair := a + "|" + b
	return strings.Contains(pair, x) && strings.Contains(pair, y)
}

func TestNormalizePersianDigits(t *testing.T) {
	got := NormalizePersian("كارت ۱۲ ماه و ٥ تومان")
	want := "کارت 12 ماه و 5 تومان"
	if got != want {
		t.Fatalf("NormalizePersian()=%q, want %q", got, want)
	}
}

func TestUnnumberedTextFallsBackToSingleClause(t *testing.T) {
	c := ParseCircular(CircularRequest{
		ID:           "NO-NUMBER",
		Title:        "متن بدون شماره",
		Text:         "این بخشنامه شماره‌بندی ندارد اما باید قابل گزارش باشد.",
		CircularType: "internal",
	})
	if len(c.Clauses) != 1 {
		t.Fatalf("clauses=%d, want 1", len(c.Clauses))
	}
	if c.Clauses[0].ID != "NO-NUMBER#1" {
		t.Fatalf("clause id=%q, want NO-NUMBER#1", c.Clauses[0].ID)
	}
}

func TestExtractClausesAcceptsCommonNumberingPunctuation(t *testing.T) {
	c := ParseCircular(CircularRequest{
		ID:    "PUNCT",
		Title: "تست",
		Text:  "بند ۱: حکم اول\nبند ۲- حکم دوم\nبند ۳. حکم سوم",
	})
	if len(c.Clauses) != 3 {
		t.Fatalf("clauses=%d, want 3", len(c.Clauses))
	}
	if c.Clauses[0].ID != "PUNCT#1" || c.Clauses[1].ID != "PUNCT#2" || c.Clauses[2].ID != "PUNCT#3" {
		t.Fatalf("clause ids=%q,%q,%q", c.Clauses[0].ID, c.Clauses[1].ID, c.Clauses[2].ID)
	}
}

func TestMetadataExtractedFromHeaders(t *testing.T) {
	c := ParseCircular(CircularRequest{Text: `شناسه بخشنامه: BX-META
عنوان: بخشنامه شرایط دسته‌چک
نوع: نظارتی/بالادستی
واحد صادرکننده: اداره ریسک
تاریخ صدور: 1404/02/03

متن بخشنامه:
بند 1) مشتریان با سطح ریسک زیاد از دریافت دسته‌چک محروم هستند.`})
	if c.ID != "BX-META" || c.Title != "بخشنامه شرایط دسته‌چک" || c.CircularType != "supervisory" || c.IssuerUnit != "اداره ریسک" || c.IssueDate != "1404/02/03" {
		t.Fatalf("metadata=%+v", c)
	}
	if c.Topic != "دسته چک" {
		t.Fatalf("topic=%q, want دسته چک", c.Topic)
	}
	if len(c.Clauses[0].Embedding) != embeddingSize {
		t.Fatalf("embedding size=%d, want %d", len(c.Clauses[0].Embedding), embeddingSize)
	}
}
