package conflict

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	clauseRE = regexp.MustCompile(`بند\s+([0-9۰-۹٠-٩]+)\s*[\)\-:.：]`)
	// ارجاع صریح «بخشنامه X» (حتی بدون رقم: PDF-BASE) + شناسه‌های BX/GT/... که رقم دارند.
	refExplicitRE = regexp.MustCompile(`بخشنامه\s+([A-Za-z][A-Za-z0-9_-]+)`)
	refIDRE       = regexp.MustCompile(`\b((?:BX|GT|PDF|UI|S)[-_A-Za-z0-9]*[0-9][A-Za-z0-9_-]*)\b`)
	amountUnitRE  = regexp.MustCompile(`([0-9۰-۹٠-٩][0-9۰-۹٠-٩٬,]*)\s*(میلیارد|میلیون)?\s*(تومان|ریال)?`)
	monthYearRE   = regexp.MustCompile(`([0-9۰-۹٠-٩]+)\s*(ماه|سال)`)
	spaceRE      = regexp.MustCompile(`\s+`)
	headerIDRE   = regexp.MustCompile(`شناسه بخشنامه:\s*(.+)`)
	titleRE      = regexp.MustCompile(`عنوان:\s*(.+)`)
	typeRE       = regexp.MustCompile(`نوع:\s*(.+)`)
	issuerRE     = regexp.MustCompile(`واحد صادرکننده:\s*(.+)`)
	dateRE       = regexp.MustCompile(`تاریخ صدور:\s*([0-9۰-۹٠-٩/.-]+)`)
)

// واژه‌اعداد فارسی رایج در سقف/مبالغ بخشنامه.
var persianAmountWords = map[string]int{
	"ده": 10, "بیست": 20, "سی": 30, "چهل": 40, "پنجاه": 50,
	"شصت": 60, "هفتاد": 70, "هشتاد": 80, "نود": 90,
	"صد": 100, "دویست": 200, "سیصد": 300, "چهارصد": 400, "پانصد": 500,
	"ششصد": 600, "هفتصد": 700, "هشتصد": 800, "نهصد": 900,
	"هزار": 1000,
}

func jalaliDateValue(s string) (int, bool) {
	parts := strings.FieldsFunc(NormalizePersian(s), func(r rune) bool {
		return r == '/' || r == '-' || r == '.'
	})
	if len(parts) != 3 {
		return 0, false
	}
	year, errYear := strconv.Atoi(parts[0])
	month, errMonth := strconv.Atoi(parts[1])
	day, errDay := strconv.Atoi(parts[2])
	if errYear != nil || errMonth != nil || errDay != nil || year < 1000 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31 {
		return 0, false
	}
	return year*10000 + month*100 + day, true
}

type seedIndex struct {
	CircularID  string `json:"circular_id"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	TypeEN      string `json:"type_en"`
	IssuingUnit string `json:"issuing_unit"`
	IssueDate   string `json:"issue_date"`
	File        string `json:"file"`
}

// SeedFromDataDir آرشیو نمونه را از data/circulars_index.json و فایل‌های متن
// بخشنامه بارگذاری می‌کند.
func SeedFromDataDir(store *Store, dataDir string) error {
	b, err := os.ReadFile(filepath.Join(dataDir, "circulars_index.json"))
	if err != nil {
		return err
	}
	var items []seedIndex
	if err := json.Unmarshal(b, &items); err != nil {
		return err
	}
	for _, item := range items {
		if store.HasCircular(item.CircularID) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dataDir, item.File))
		if err != nil {
			return err
		}
		req := CircularRequest{
			ID:           item.CircularID,
			Title:        item.Title,
			Text:         string(raw),
			IssuerUnit:   item.IssuingUnit,
			CircularType: item.TypeEN,
			IssueDate:    item.IssueDate,
			Topic:        item.Title,
		}
		store.UpsertCircular(ParseCircular(req))
	}
	return nil
}

// ParseCircular ورودی ثبت بخشنامه را به مدل داخلی همراه با بندهای استخراج‌شده
// تبدیل می‌کند.
func ParseCircular(req CircularRequest) Circular {
	now := time.Now().UTC()
	raw := strings.TrimSpace(req.Text)
	id := firstNonEmpty(req.ID, findHeader(raw, headerIDRE))
	title := firstNonEmpty(req.Title, findHeader(raw, titleRE))
	typ := normalizeType(firstNonEmpty(req.CircularType, findHeader(raw, typeRE)))
	c := Circular{
		ID:             id,
		Title:          title,
		RawText:        raw,
		NormalizedText: NormalizePersian(raw),
		IssuerUnit:     firstNonEmpty(req.IssuerUnit, findHeader(raw, issuerRE)),
		CircularType:   typ,
		HierarchyLevel: hierarchyLevel(typ),
		Topic:          req.Topic,
		IssueDate:      firstNonEmpty(req.IssueDate, findHeader(raw, dateRE)),
		Status:         "active",
		CreatedAt:      now,
	}
	c.Clauses = ExtractClauses(c)
	if c.Topic == "" {
		c.Topic = inferTopic(c)
	}
	// Neural embeddings when OPENAI_* configured; keeps local vectors on failure/offline.
	EnrichCircularEmbeddings(&c)
	return c
}

// ExtractClauses متن بخشنامه را بر اساس الگوی «بند N)» خرد می‌کند.
//
// اگر شماره‌بندی وجود نداشته باشد کل متن به عنوان بند ۱ ثبت می‌شود تا ورودی
// نامنظم هم قابل تحلیل و گزارش باشد.
func ExtractClauses(c Circular) []Clause {
	matches := clauseRE.FindAllStringSubmatchIndex(c.RawText, -1)
	if len(matches) == 0 {
		return []Clause{buildClause(c, "1", c.RawText)}
	}
	out := make([]Clause, 0, len(matches))
	for i, m := range matches {
		start := m[0]
		end := len(c.RawText)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		num := c.RawText[m[2]:m[3]]
		out = append(out, buildClause(c, asciiDigits(num), c.RawText[start:end]))
	}
	return out
}

func buildClause(c Circular, number, text string) Clause {
	norm := NormalizePersian(text)
	subj := subject(norm)
	if subj == "عمومی" {
		subj = subject(c.Title + " " + c.Topic)
	}
	return Clause{
		ID:                    c.ID + "#" + number,
		CircularID:            c.ID,
		ClauseNumber:          number,
		OriginalText:          strings.TrimSpace(text),
		NormalizedText:        norm,
		Subject:               subj,
		RulingType:            rulingType(norm),
		ExtractedConditions:   conditions(norm),
		ReferencedCircularIDs: refs(norm),
		// Local vector first (fast/offline); EnrichCircularEmbeddings upgrades to neural in batch.
		Embedding: buildLocalEmbedding(norm),
	}
}

func inferTopic(c Circular) string {
	for _, cl := range c.Clauses {
		if cl.Subject != "عمومی" {
			return cl.Subject
		}
	}
	return c.Title
}

// NormalizePersian متن فارسی را برای جست‌وجو و مقایسه یکنواخت می‌کند.
func NormalizePersian(s string) string {
	replacer := strings.NewReplacer(
		"ي", "ی", "ك", "ک", "ة", "ه",
		"٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4",
		"٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9",
		"۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4",
		"۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9",
		"٬", ",", "‌", " ",
	)
	return strings.TrimSpace(spaceRE.ReplaceAllString(replacer.Replace(s), " "))
}

func asciiDigits(s string) string {
	out := NormalizePersian(s)
	out = strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, out)
	return out
}

func subject(s string) string {
	s = NormalizePersian(s)
	if containsAny(s, "ابزارهای اعتباری", "خدمات اعتباری", "محصول اعتباری", "محصولات اعتباری") || creditKeywordCount(s) > 1 {
		return "ابزارهای اعتباری"
	}
	// ترتیب مهم است: عبارات خاص‌تر قبل از کلی‌تر.
	keywords := []string{
		"دسته چک", "دسته‌چک", "کارت اعتباری", "کارت خرید اقساطی", "وام مصرفی",
		"قرض الحسنه", "قرض‌الحسنه", "سپرده", "RBCI", "مشتری جدید", "فاقد سابقه",
		"تسهیلات مصرفی", "سقف تسهیلات", "تسهیلات", "وام", "سقف کارت", "کارت",
	}
	for _, k := range keywords {
		if strings.Contains(s, NormalizePersian(k)) {
			switch NormalizePersian(k) {
			case "دسته‌چک":
				return "دسته چک"
			case "قرض‌الحسنه":
				return "قرض الحسنه"
			case "سقف تسهیلات", "تسهیلات مصرفی", "وام":
				return "تسهیلات"
			case "سقف کارت", "کارت":
				return "کارت اعتباری"
			default:
				return NormalizePersian(k)
			}
		}
	}
	return "عمومی"
}

func rulingType(s string) string {
	switch {
	case containsAny(s, "لغو", "اصلاح", "جایگزین", "نسخ"):
		return "amendment"
	case containsAny(s, "مستثنا"):
		return "exception"
	case containsAny(s, "محروم", "ممنوع", "تحت هیچ شرایطی", "ارائه نمی شود", "تعلق نمی گیرد", "نباید"):
		return "prohibition"
	case containsAny(s, "باید", "الزامی", "منوط", "صرفا در صورت"):
		return "obligation"
	case containsAny(s, "مجاز", "قابل ارائه", "امکان پذیر", "می توانند"):
		return "permission"
	// سقف/حداکثر مبلغ حکم الزام‌آور آستانه‌ای است نه تعریف خنثی.
	case containsAny(s, "سقف", "حداکثر", "حداقل") && (containsAny(s, "تومان", "ریال", "میلیون", "میلیارد") || hasDigit(s)):
		return "obligation"
	default:
		return "definition"
	}
}

func creditKeywordCount(s string) int {
	count := 0
	for _, k := range []string{"دسته چک", "کارت اعتباری", "وام مصرفی", "کارت خرید اقساطی", "تسهیلات"} {
		if strings.Contains(s, NormalizePersian(k)) {
			count++
		}
	}
	return count
}

func conditions(s string) map[string]any {
	s = NormalizePersian(s)
	out := map[string]any{}
	if strings.Contains(s, "ریسک") {
		if strings.Contains(s, "زیاد") {
			out["risk"] = "high"
		} else if strings.Contains(s, "متوسط") {
			out["risk"] = "medium"
		}
	}
	if strings.Contains(s, "ضامن") {
		out["guarantor"] = true
	}
	if strings.Contains(s, "وثیقه") {
		out["collateral"] = true
	}
	for _, m := range monthYearRE.FindAllStringSubmatch(s, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		switch m[2] {
		case "ماه":
			out["months"] = n
		case "سال":
			out["years"] = n
		}
	}
	if amount, ok := extractAmountToman(s); ok {
		out["amount_toman"] = amount
	}
	return out
}

// extractAmountToman مبلغ را به تومان نرمال می‌کند (ریال÷۱۰، میلیون/میلیارد، واژه‌عدد).
func extractAmountToman(s string) (int, bool) {
	s = NormalizePersian(s)
	// 1) عدد + (میلیون|میلیارد)? + (تومان|ریال)?
	best := 0
	found := false
	for _, m := range amountUnitRE.FindAllStringSubmatch(s, -1) {
		raw := strings.ReplaceAll(m[1], ",", "")
		n, err := strconv.Atoi(raw)
		if err != nil || n == 0 {
			continue
		}
		scale := m[2]
		unit := m[3]
		// «N ماه/سال» را مبلغ حساب نکن.
		if unit == "" && scale == "" {
			// فقط وقتی واحد پولی یا مقیاس بزرگ هست، یا متن صریحاً سقف/مبلغ دارد.
			if !containsAny(s, "سقف", "مبلغ", "تومان", "ریال", "میلیون", "میلیارد") {
				continue
			}
			// عددهای خیلی کوچک بدون واحد (مثل شماره بند) را رد کن.
			if n < 1000 {
				continue
			}
		}
		val := n
		switch scale {
		case "میلیون":
			val = n * 1_000_000
		case "میلیارد":
			val = n * 1_000_000_000
		}
		switch unit {
		case "ریال":
			val = val / 10
		case "تومان", "":
			// بدون واحد ولی با میلیون/میلیارد یا سقف → تومان فرض می‌شود مگر ریال در جمله باشد.
			if unit == "" && strings.Contains(s, "ریال") && !strings.Contains(s, "تومان") {
				val = val / 10
			}
		}
		if val > best {
			best = val
			found = true
		}
	}
	// 2) واژه‌عدد + میلیون/میلیارد (پانصد میلیون ریال / یک میلیارد تومان)
	if wordVal, ok := persianWordAmountToman(s); ok && wordVal > best {
		best = wordVal
		found = true
	}
	return best, found
}

func persianWordAmountToman(s string) (int, bool) {
	s = NormalizePersian(s)
	unitMul := 0
	switch {
	case strings.Contains(s, "میلیارد"):
		unitMul = 1_000_000_000
	case strings.Contains(s, "میلیون"):
		unitMul = 1_000_000
	default:
		return 0, false
	}
	coef := 0
	// «یک میلیارد/میلیون» قبل از واژه‌اعداد چندرقمی
	if strings.Contains(s, "یک میلیارد") || strings.Contains(s, "یک میلیون") {
		coef = 1
	}
	for word, val := range persianAmountWords {
		if strings.Contains(s, word) && val > coef {
			coef = val
		}
	}
	if coef == 0 {
		return 0, false
	}
	val := coef * unitMul
	if strings.Contains(s, "ریال") && !strings.Contains(s, "تومان") {
		val = val / 10
	}
	return val, true
}

func hasDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func refs(s string) []string {
	s = NormalizePersian(s)
	seen := map[string]bool{}
	out := []string{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	for _, m := range refExplicitRE.FindAllStringSubmatch(s, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	for _, m := range refIDRE.FindAllStringSubmatch(s, -1) {
		if len(m) >= 2 {
			add(m[1])
		}
	}
	return out
}

func tokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, f := range strings.Fields(NormalizePersian(s)) {
		f = strings.TrimFunc(f, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})
		if len([]rune(f)) >= 3 {
			out[f] = true
		}
	}
	return out
}

func overlapScore(a, b string) float64 {
	at, bt := tokens(a), tokens(b)
	if len(at) == 0 || len(bt) == 0 {
		return 0
	}
	hit := 0
	for k := range at {
		if bt[k] {
			hit++
		}
	}
	return float64(hit) / float64(min(len(at), len(bt)))
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, NormalizePersian(n)) {
			return true
		}
	}
	return false
}

func findHeader(s string, re *regexp.Regexp) string {
	if m := re.FindStringSubmatch(s); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func normalizeType(t string) string {
	t = NormalizePersian(strings.ToLower(t))
	if containsAny(t, "supervisory", "regulatory", "نظارتی", "بالادستی") {
		return "supervisory"
	}
	return "internal"
}

func hierarchyLevel(t string) int {
	if t == "supervisory" {
		return 2
	}
	return 1
}
