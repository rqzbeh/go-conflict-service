package conflict

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Analyze یک بخشنامه را با آرشیو موجود مقایسه و گزارش تعارض می‌سازد.
func Analyze(store *Store, circularID string) (Report, error) {
	source, ok := store.Circular(circularID)
	if !ok {
		return Report{}, ErrNotFound
	}
	var rels []Relationship
	all := store.ListCirculars()
	for _, candidate := range all {
		if candidate.ID == source.ID {
			continue
		}
		for _, a := range source.Clauses {
			for _, b := range candidate.Clauses {
				if !isCandidate(a, b) {
					continue
				}
				rel := compare(source, a, candidate, b)
				if rel.RelationshipType != "neutral" {
					rels = append(rels, resolve(source, candidate, rel))
				}
			}
		}
	}
	report := BuildReport(circularID, rels)
	store.SaveReport(report)
	return report, nil
}

// ScanArchive همه جفت‌های مرتبط در آرشیو را برای یافتن تعارض‌های پنهان بررسی
// می‌کند.
func ScanArchive(store *Store) Report {
	all := store.ListCirculars()
	var rels []Relationship
	for i, a := range all {
		for j := i + 1; j < len(all); j++ {
			b := all[j]
			for _, ac := range a.Clauses {
				for _, bc := range b.Clauses {
					if !isCandidate(ac, bc) {
						continue
					}
					rel := compareDeterministic(a, ac, b, bc)
					if rel.RelationshipType != "neutral" {
						rels = append(rels, resolve(a, b, rel))
					}
				}
			}
		}
	}
	report := BuildReport("archive", rels)
	store.SaveReport(report)
	return report
}

// BuildReport روابط را همراه با خلاصه ساده حقوقی/تطبیق بسته‌بندی می‌کند.
func BuildReport(circularID string, rels []Relationship) Report {
	lines, byLLM := SummarizeForCompliance(rels)
	return Report{
		CircularID:            circularID,
		Summary:               summarize(rels),
		PlainLanguageSummary:  lines,
		SummaryGeneratedByLLM: byLLM,
		Relationships:         rels,
	}
}

func isCandidate(a, b Clause) bool {
	// اصلاحی که صریحاً «بند N بخشنامه X» را هدف می‌گیرد فقط با همان بند مقایسه می‌شود.
	if amendmentTargetsOtherClause(a, b) || amendmentTargetsOtherClause(b, a) {
		return false
	}
	if referencesCircular(a, b.CircularID) || referencesCircular(b, a.CircularID) {
		return true
	}
	// اصلاح/نسخ با هم‌پوشانی متنی کافی — حتی اگر subject هنوز عمومی باشد.
	if (a.RulingType == "amendment" || b.RulingType == "amendment") &&
		overlapScore(a.NormalizedText, b.NormalizedText) >= 0.35 {
		return true
	}
	if sameSubject(a, b) {
		return true
	}
	if broadCredit(a) && creditOrRisk(b) || broadCredit(b) && creditOrRisk(a) {
		return true
	}
	if embeddingScore(a.Embedding, b.Embedding) >= 0.42 && sameProductFamily(a, b) {
		return true
	}
	// آستانه مبلغ/ماه متفاوت با هم‌پوشانی معقول = کاندید تعارض جزئی
	if thresholdConflict(a, b) && overlapScore(a.NormalizedText, b.NormalizedText) >= 0.35 {
		return true
	}
	return overlapScore(a.NormalizedText, b.NormalizedText) >= 0.45 && sameProductFamily(a, b)
}

// amendmentTargetsOtherClause: a اصلاح بخشنامه b است ولی بند دیگری غیر از b را نام برده.
func amendmentTargetsOtherClause(a, b Clause) bool {
	if a.RulingType != "amendment" {
		return false
	}
	if !referencesCircular(a, b.CircularID) &&
		!strings.Contains(a.NormalizedText, strings.ToLower(b.CircularID)) &&
		!strings.Contains(a.NormalizedText, b.CircularID) {
		return false
	}
	n, ok := referencedClauseNumber(a.NormalizedText, b.CircularID)
	if !ok {
		return false
	}
	return b.ClauseNumber != n && b.ClauseNumber != asciiDigits(n)
}

func compare(aCirc Circular, a Clause, bCirc Circular, b Clause) Relationship {
	typ, confidence, rationale := classify(a, b)
	return buildRelationship(aCirc, a, bCirc, b, typ, confidence, rationale)
}

func compareDeterministic(aCirc Circular, a Clause, bCirc Circular, b Clause) Relationship {
	result := classifyDeterministic(a, b)
	return buildRelationship(aCirc, a, bCirc, b, result.RelationshipType, result.Confidence, result.Rationale)
}

func buildRelationship(aCirc Circular, a Clause, bCirc Circular, b Clause, typ string, confidence float64, rationale string) Relationship {
	return Relationship{
		ID:               fmt.Sprintf("%s__%s", a.ID, b.ID),
		SourceClauseID:   a.ID,
		TargetClauseID:   b.ID,
		RelationshipType: typ,
		Confidence:       confidence,
		Rationale:        rationale,
		Evidence: map[string]any{
			"source_circular": aCirc.ID,
			"target_circular": bCirc.ID,
			"source_text":     a.OriginalText,
			"target_text":     b.OriginalText,
		},
		ResolverStatus: "unresolved",
		RequiredAction: "review",
		CreatedAt:      time.Now().UTC(),
	}
}

func classify(a, b Clause) (string, float64, string) {
	return classifyWithOptionalLLM(a, b)
}

func resolve(a, b Circular, rel Relationship) Relationship {
	if rel.RelationshipType == "overlap_without_conflict" {
		rel.ResolverStatus = "compatible"
		rel.RequiredAction = "accept"
		return rel
	}
	winner := ""
	switch {
	case a.HierarchyLevel > b.HierarchyLevel:
		winner = rel.SourceClauseID
	case b.HierarchyLevel > a.HierarchyLevel:
		winner = rel.TargetClauseID
	case issueDateAfter(a.IssueDate, b.IssueDate):
		winner = rel.SourceClauseID
	case issueDateAfter(b.IssueDate, a.IssueDate):
		winner = rel.TargetClauseID
	}
	if winner == "" {
		rel.ResolverStatus = "needs_review"
		rel.RequiredAction = "review"
		return rel
	}
	rel.WinningClauseID = winner
	rel.ResolverStatus = "resolved"
	if rel.RelationshipType == "supersession" {
		rel.RequiredAction = "mark_superseded"
	} else {
		rel.RequiredAction = "amend_or_reject_losing_clause"
	}
	return rel
}

func explicitSupersession(a, b Clause) bool {
	if a.RulingType != "amendment" {
		return false
	}
	// ارجاع صریح به شناسه بخشنامه هدف = نسخ/اصلاح
	if referencesCircular(a, b.CircularID) || strings.Contains(a.NormalizedText, strings.ToLower(b.CircularID)) || strings.Contains(a.NormalizedText, b.CircularID) {
		// اگر متن فقط «بند N بخشنامه X» را نام برد، فقط همان شماره بند هدف.
		if n, ok := referencedClauseNumber(a.NormalizedText, b.CircularID); ok {
			return b.ClauseNumber == n || b.ClauseNumber == asciiDigits(n)
		}
		return true
	}
	// بدون ارجاع شناسه، فقط اگر هم‌موضوع و هم‌پوشانی متنی بالا باشد.
	return sameSubject(a, b) && overlapScore(a.NormalizedText, b.NormalizedText) >= 0.55
}

// referencedClauseNumber extracts N from patterns like «بند 2 بخشنامه ID» / «بند ۲ از ID».
func referencedClauseNumber(text, circularID string) (string, bool) {
	text = NormalizePersian(text)
	id := NormalizePersian(circularID)
	if id == "" || !strings.Contains(text, id) {
		return "", false
	}
	// بند N ... ID  or  بند N بخشنامه ID
	re := regexp.MustCompile(`بند\s+([0-9۰-۹٠-٩]+)\s*(?:بخشنامه\s+)?` + regexp.QuoteMeta(id))
	if m := re.FindStringSubmatch(text); len(m) == 2 {
		return asciiDigits(m[1]), true
	}
	// ID ... بند N
	re2 := regexp.MustCompile(regexp.QuoteMeta(id) + `[^۰-۹0-9]{0,40}بند\s+([0-9۰-۹٠-٩]+)`)
	if m := re2.FindStringSubmatch(text); len(m) == 2 {
		return asciiDigits(m[1]), true
	}
	return "", false
}

func referencesCircular(a Clause, circularID string) bool {
	if circularID == "" {
		return false
	}
	for _, ref := range a.ReferencedCircularIDs {
		if strings.EqualFold(ref, circularID) {
			return true
		}
	}
	// fallback متنی برای شناسه‌هایی که parser از دست داده
	return strings.Contains(a.NormalizedText, circularID) ||
		strings.Contains(a.NormalizedText, strings.ToLower(circularID)) ||
		strings.Contains(strings.ToLower(a.OriginalText), strings.ToLower(circularID))
}

func exceptionAgainstProhibition(a, b Clause) bool {
	// استثنای صریح در برابر ممنوعیت فقط وقتی تعارض جزئی است که هر دو بند
	// دامنه ریسک/موضوع هم‌تراز داشته باشند؛ وگرنه استثنای محصولی با ممنوعیت
	// بالادستی لزوماً تعارض نیست (مثلاً قرض‌الحسنه در برابر محرومیت اعتباری).
	if !((a.RulingType == "exception" && b.RulingType == "prohibition") || (a.RulingType == "prohibition" && b.RulingType == "exception")) {
		return false
	}
	if !sameScope(a, b) {
		return false
	}
	return sameRiskScope(a, b) || sameSubject(a, b)
}

func oppositeRulings(a, b string) bool {
	return (a == "permission" && b == "prohibition") || (a == "prohibition" && b == "permission")
}

func sameScope(a, b Clause) bool {
	if amendmentTargetsOtherClause(a, b) || amendmentTargetsOtherClause(b, a) {
		return false
	}
	if sameSubject(a, b) {
		return true
	}
	if referencesCircular(a, b.CircularID) || referencesCircular(b, a.CircularID) {
		return true
	}
	if broadCredit(a) && creditOrRisk(b) || broadCredit(b) && creditOrRisk(a) {
		return true
	}
	// آستانه‌های متفاوت روی متن هم‌پوشان (سقف/ماه) حتی با subject عمومی
	if thresholdConflict(a, b) && overlapScore(a.NormalizedText, b.NormalizedText) >= 0.40 {
		return true
	}
	return overlapScore(a.NormalizedText, b.NormalizedText) >= 0.50 && sameProductFamily(a, b)
}

func thresholdConflict(a, b Clause) bool {
	// مبلغ/ماه در متن اصلاح جزئی نباید با بندهای غیرهدف همان بخشنامه تعارض شود.
	if amendmentTargetsOtherClause(a, b) || amendmentTargetsOtherClause(b, a) {
		return false
	}
	for _, key := range []string{"amount_toman", "months", "years"} {
		av, aok := asInt(a.ExtractedConditions[key])
		bv, bok := asInt(b.ExtractedConditions[key])
		if aok && bok && av != bv {
			return true
		}
	}
	return false
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	default:
		return 0, false
	}
}

func sameRiskScope(a, b Clause) bool {
	av, aok := a.ExtractedConditions["risk"]
	bv, bok := b.ExtractedConditions["risk"]
	if aok && bok {
		return av == bv
	}
	return !aok && !bok
}

func summarize(rels []Relationship) map[string]int {
	out := map[string]int{}
	for _, r := range rels {
		out[r.RelationshipType]++
	}
	out["total"] = len(rels)
	return out
}

func issueDateAfter(a, b string) bool {
	av, aok := jalaliDateValue(a)
	bv, bok := jalaliDateValue(b)
	return aok && bok && av > bv
}

func sameSubject(a, b Clause) bool {
	return a.Subject != "عمومی" && a.Subject == b.Subject
}

func broadCredit(c Clause) bool {
	return c.Subject == "ابزارهای اعتباری" || containsAny(c.NormalizedText, "ابزارهای اعتباری", "خدمات اعتباری", "محصول اعتباری", "محصولات اعتباری")
}

func creditOrRisk(c Clause) bool {
	return c.ExtractedConditions["risk"] != nil || sameProductFamily(c, Clause{Subject: "ابزارهای اعتباری"})
}

func sameProductFamily(a, b Clause) bool {
	// قرض‌الحسنه و سپرده استثناهای بخشنامه مشتریان جدید هستند و نباید فقط به
	// خاطر واژه «اعتباری» در بند بالادستی هم‌خانواده ابزارهای اعتباری شوند.
	if isExemptBaseProduct(a) || isExemptBaseProduct(b) {
		return a.Subject != "عمومی" && a.Subject == b.Subject
	}
	if broadCredit(a) || broadCredit(b) {
		return true
	}
	if a.Subject != "عمومی" && a.Subject == b.Subject {
		return true
	}
	// تسهیلات / وام مصرفی / کارت در یک خانواده اعتباری عام
	family := func(s string) string {
		switch s {
		case "تسهیلات", "وام مصرفی", "کارت اعتباری", "کارت خرید اقساطی", "دسته چک", "ابزارهای اعتباری":
			return "credit"
		default:
			return s
		}
	}
	fa, fb := family(a.Subject), family(b.Subject)
	return fa != "عمومی" && fa == fb
}

func isExemptBaseProduct(c Clause) bool {
	return containsAny(c.NormalizedText, "قرض الحسنه", "قرض‌الحسنه", "سپرده", "حساب فعال") ||
		c.Subject == "قرض‌الحسنه" || c.Subject == "سپرده"
}
