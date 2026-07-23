package conflict

import "time"

// Circular ساختار کامل یک بخشنامه ثبت‌شده است.
//
// متن خام برای نمایش شاهد نگه داشته می‌شود و متن نرمال‌شده برای جست‌وجو و
// مقایسه استفاده می‌شود. سطح سلسله‌مراتب در Resolver تعیین می‌کند کدام بند
// در تعارض‌های قطعی برنده است.
type Circular struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	RawText        string    `json:"raw_text"`
	NormalizedText string    `json:"normalized_text"`
	IssuerUnit     string    `json:"issuer_unit"`
	CircularType   string    `json:"circular_type"`
	HierarchyLevel int       `json:"hierarchy_level"`
	Topic          string    `json:"topic"`
	IssueDate      string    `json:"issue_date"`
	EffectiveDate  string    `json:"effective_date,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	Clauses        []Clause  `json:"clauses"`
}

// Clause یک بند آدرس‌پذیر از بخشنامه است؛ مثل BX-1007#1.
//
// فیلدهای استخراج‌شده عمداً ساده نگه داشته شده‌اند تا تصمیم‌های عددی و حقوقی
// قابل تست باشند و رفتار مدل زبانی وارد منطق تقدم نشود.
type Clause struct {
	ID                    string         `json:"id"`
	CircularID            string         `json:"circular_id"`
	ClauseNumber          string         `json:"clause_number"`
	OriginalText          string         `json:"original_text"`
	NormalizedText        string         `json:"normalized_text"`
	Subject               string         `json:"subject"`
	RulingType            string         `json:"ruling_type"`
	ExtractedConditions   map[string]any `json:"extracted_conditions_json"`
	ReferencedCircularIDs []string       `json:"referenced_circular_ids"`
	Embedding             []float64      `json:"embedding,omitempty"`
}

// Relationship نتیجه مقایسه دو بند است.
//
// Evidence متن دقیق دو بند را نگه می‌دارد تا گزارش برای واحد حقوقی/تطبیق
// قابل بازبینی باشد.
type Relationship struct {
	ID               string         `json:"id"`
	SourceClauseID   string         `json:"source_clause_id"`
	TargetClauseID   string         `json:"target_clause_id"`
	RelationshipType string         `json:"relationship_type"`
	Confidence       float64        `json:"confidence"`
	Rationale        string         `json:"rationale"`
	Evidence         map[string]any `json:"evidence_json"`
	ResolverStatus   string         `json:"resolver_status"`
	WinningClauseID  string         `json:"winning_clause_id,omitempty"`
	RequiredAction   string         `json:"required_action"`
	ReviewStatus     string         `json:"review_status,omitempty"`
	ReviewedBy       string         `json:"reviewed_by,omitempty"`
	ReviewNote       string         `json:"review_note,omitempty"`
	ReviewedAt       *time.Time     `json:"reviewed_at,omitempty"`
	DeepReview       *DeepReview    `json:"deep_review,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
}

// DeepReview خروجی بررسی عمیق هوش مصنوعی برای یک رابطه است.
type DeepReview struct {
	Verdict           string    `json:"verdict"`
	Severity          string    `json:"severity"`
	PlainExplanation  string    `json:"plain_explanation"`
	LegalReason       string    `json:"legal_reason"`
	RecommendedAction string    `json:"recommended_action"`
	Questions         []string  `json:"questions,omitempty"`
	GeneratedByLLM    bool      `json:"generated_by_llm"`
	CreatedAt         time.Time `json:"created_at"`
}

// Report خروجی نهایی تحلیل یک بخشنامه یا اسکن آرشیو است.
type Report struct {
	CircularID            string         `json:"circular_id"`
	Summary               map[string]int `json:"summary"`
	PlainLanguageSummary  []string       `json:"plain_language_summary,omitempty"`
	SummaryGeneratedByLLM bool           `json:"summary_generated_by_llm"`
	Relationships         []Relationship `json:"relationships"`
}

// CircularRequest ورودی API ثبت بخشنامه است.
type CircularRequest struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Text         string `json:"text"`
	IssuerUnit   string `json:"issuer_unit"`
	CircularType string `json:"circular_type"`
	IssueDate    string `json:"issue_date"`
	Topic        string `json:"topic"`
}

// ClauseUpdateRequest ورودی اصلاح دستی متن یک بند است.
type ClauseUpdateRequest struct {
	Text string `json:"text"`
}

// ReviewRequest تصمیم انسانی واحد حقوقی/تطبیق روی یک رابطه است.
type ReviewRequest struct {
	Status string `json:"status"`
	By     string `json:"by"`
	Note   string `json:"note"`
}
