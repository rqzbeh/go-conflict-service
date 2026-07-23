package conflict

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultLLMTimeout = 20 * time.Second

var validRelationshipTypes = map[string]bool{
	"full_contradiction":       true,
	"partial_contradiction":    true,
	"overlap_without_conflict": true,
	"supersession":             true,
	"neutral":                  true,
}

// LLMConfig تنظیمات اتصال به API سازگار با OpenAI است.
//
// کلید API فقط از محیط خوانده می‌شود و نباید در فایل‌های پروژه ذخیره شود.
type LLMConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

// LLMClient مقایسه معنایی بندها را از مسیر Chat Completions انجام می‌دهد.
type LLMClient struct {
	cfg        LLMConfig
	httpClient *http.Client
}

type llmResult struct {
	RelationshipType string  `json:"relationship_type"`
	Confidence       float64 `json:"confidence"`
	Rationale        string  `json:"rationale"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
		Delta   chatMessage `json:"delta"`
	} `json:"choices"`
}

type complianceSummaryResponse struct {
	Items []string `json:"items"`
}

type deepReviewResponse struct {
	Verdict           string   `json:"verdict"`
	Severity          string   `json:"severity"`
	PlainExplanation  string   `json:"plain_explanation"`
	LegalReason       string   `json:"legal_reason"`
	RecommendedAction string   `json:"recommended_action"`
	Questions         []string `json:"questions"`
}

type eligibilitySummaryResponse struct {
	Summary string `json:"summary"`
}

// LLMConfigFromEnv تنظیمات LLM را از متغیرهای محیطی می‌خواند.
func LLMConfigFromEnv() (LLMConfig, bool) {
	cfg := LLMConfig{
		BaseURL: strings.TrimRight(os.Getenv("OPENAI_BASE_URL"), "/"),
		APIKey:  os.Getenv("OPENAI_API_KEY"),
		Model:   os.Getenv("OPENAI_MODEL"),
		Timeout: defaultLLMTimeout,
	}
	if raw := os.Getenv("OPENAI_TIMEOUT_SECONDS"); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			cfg.Timeout = time.Duration(seconds) * time.Second
		}
	}
	return cfg, cfg.BaseURL != "" && cfg.APIKey != "" && cfg.Model != ""
}

// NewLLMClient یک کلاینت OpenAI-compatible می‌سازد.
func NewLLMClient(cfg LLMConfig) *LLMClient {
	return &LLMClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// LLMRuntimeStatus وضعیت قابل نمایش LLM را بدون افشای کلید برمی‌گرداند.
func LLMRuntimeStatus() map[string]any {
	cfg, ok := LLMConfigFromEnv()
	return map[string]any{
		"enabled": ok,
		"model":   cfg.Model,
		"base_url": func() string {
			if cfg.BaseURL == "" {
				return ""
			}
			return cfg.BaseURL
		}(),
	}
}

func classifyWithOptionalLLM(a, b Clause) (string, float64, string) {
	// Default path is deterministic. Per-pair LLM during Analyze can hang for minutes
	// when OPENAI is slow. Opt-in only: LLM_CLASSIFY=1, and only for borderline pairs.
	fallback := classifyDeterministic(a, b)
	if !llmClassifyEnabled() {
		return fallback.RelationshipType, fallback.Confidence, fallback.Rationale
	}
	switch fallback.RelationshipType {
	case "full_contradiction", "partial_contradiction", "supersession":
		return fallback.RelationshipType, fallback.Confidence, fallback.Rationale
	}
	cfg, ok := LLMConfigFromEnv()
	if !ok {
		return fallback.RelationshipType, fallback.Confidence, fallback.Rationale
	}
	if cfg.Timeout > 4*time.Second {
		cfg.Timeout = 4 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	got, err := NewLLMClient(cfg).Compare(ctx, a, b, fallback)
	if err != nil {
		return fallback.RelationshipType, fallback.Confidence, fallback.Rationale
	}
	chosen := chooseClassification(fallback, got)
	return chosen.RelationshipType, chosen.Confidence, chosen.Rationale
}

// llmClassifyEnabled is off by default; set LLM_CLASSIFY=1 to allow borderline LLM classify.
func llmClassifyEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("LLM_CLASSIFY")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// SummarizeForCompliance مغایرت‌های مهم را به زبان ساده برای واحد حقوقی/تطبیق
// خلاصه می‌کند. اگر LLM در دسترس باشد، خلاصه با مدل ساخته می‌شود؛ در غیر این
// صورت متن قاعده‌محور کوتاه برمی‌گردد.
func SummarizeForCompliance(rels []Relationship) ([]string, bool) {
	important := importantRelationships(rels, 8)
	if len(important) == 0 {
		return nil, false
	}
	cfg, ok := LLMConfigFromEnv()
	if !ok {
		return deterministicComplianceSummary(important), false
	}
	lines, err := NewLLMClient(cfg).SummarizeCompliance(context.Background(), important)
	if err != nil || len(lines) == 0 {
		return deterministicComplianceSummary(important), false
	}
	return lines, true
}

func SummarizeEligibilityForLegal(risk RiskProfile, items []EligibilityItem) (string, bool) {
	fallback := deterministicEligibilitySummary(risk, items)
	cfg, ok := LLMConfigFromEnv()
	if !ok {
		return fallback, false
	}
	got, err := NewLLMClient(cfg).SummarizeEligibility(context.Background(), risk, items)
	if err != nil || strings.TrimSpace(got) == "" {
		return fallback, false
	}
	return got, true
}

func (c *LLMClient) SummarizeEligibility(ctx context.Context, risk RiskProfile, items []EligibilityItem) (string, error) {
	payload, err := json.Marshal(struct {
		Risk  RiskProfile       `json:"risk"`
		Items []EligibilityItem `json:"eligibility"`
	}{Risk: risk, Items: items})
	if err != nil {
		return "", err
	}
	prompt := "برای واحد حقوقی/تطبیق بانک، نتیجه اهلیت زیر را در یک پاراگراف فارسی ساده، دقیق و بدون اغراق خلاصه کن. حتما دلیل رد/شرط‌های اصلی و ارجاع‌های بخشنامه‌ای را ذکر کن. فقط JSON بده: {\"summary\":\"...\"}\n" + string(payload)
	body := chatRequest{
		Model:       c.cfg.Model,
		Temperature: 0.1,
		Messages: []chatMessage{
			{Role: "system", Content: "تو دستیار تطبیق بانکی هستی و فقط JSON معتبر تولید می‌کنی."},
			{Role: "user", Content: prompt},
		},
	}
	reqBody, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("llm status %d", res.StatusCode)
	}
	content, err := decodeChatContent(res.Body)
	if err != nil {
		return "", err
	}
	var parsed eligibilitySummaryResponse
	if err := json.Unmarshal([]byte(cleanJSONContent(content)), &parsed); err != nil {
		return "", err
	}
	return strings.TrimSpace(parsed.Summary), nil
}

func deterministicEligibilitySummary(risk RiskProfile, items []EligibilityItem) string {
	counts := map[string]int{}
	examples := []string{}
	for _, item := range items {
		counts[item.Decision]++
		if item.Decision != "eligible" {
			refs := make([]string, 0, len(item.Evidence))
			for _, ev := range item.Evidence {
				refs = append(refs, ev.CircularID+"#"+ev.Clause)
			}
			examples = append(examples, item.ProductFA+": "+item.Reason+" ("+strings.Join(refs, "، ")+")")
		}
	}
	return fmt.Sprintf("خلاصه حقوقی/تطبیق: سطح ریسک مشتری %s با امتیاز %d است. %d محصول مجاز، %d محصول مشروط و %d محصول غیرمجاز تشخیص داده شد. موارد اصلی: %s", risk.RiskLevelFA, risk.RiskScore, counts["eligible"], counts["conditional"], counts["not_eligible"], strings.Join(examples, "؛ "))
}

func classifyDeterministic(a, b Clause) llmResult {
	if explicitSupersession(a, b) || explicitSupersession(b, a) {
		return llmResult{"supersession", 0.9, "یکی از بندها به لغو، اصلاح یا جایگزینی بخشنامه دیگر اشاره دارد."}
	}
	if exceptionAgainstProhibition(a, b) {
		return llmResult{"partial_contradiction", 0.78, "یک بند ممنوعیت کلی دارد و بند دیگر برای همان موضوع استثنا یا مجوز ذکر می‌کند."}
	}
	if oppositeRulings(a.RulingType, b.RulingType) && sameScope(a, b) && sameRiskScope(a, b) {
		return llmResult{"full_contradiction", 0.82, "نوع حکم دو بند برای موضوع مشترک ناسازگار است."}
	}
	if thresholdConflict(a, b) && sameScope(a, b) {
		return llmResult{"partial_contradiction", 0.75, "مقادیر، سقف‌ها یا دوره‌های زمانی برای موضوع مشترک متفاوت هستند."}
	}
	if sameScope(a, b) {
		return llmResult{"overlap_without_conflict", 0.62, "دو بند موضوع مشترک دارند اما ناسازگاری صریحی پیدا نشد."}
	}
	return llmResult{"neutral", 0.2, "ارتباط مؤثر یافت نشد."}
}

func chooseClassification(fallback, got llmResult) llmResult {
	got.Rationale = "LLM: " + got.Rationale
	if fallback.RelationshipType == "full_contradiction" || fallback.RelationshipType == "partial_contradiction" {
		if got.RelationshipType == "neutral" || got.RelationshipType == "overlap_without_conflict" {
			return fallback
		}
	}
	if got.Confidence < 0.55 {
		return fallback
	}
	return got
}

// Compare دو بند را به مدل می‌فرستد و خروجی JSON محدودشده دریافت می‌کند.
func (c *LLMClient) Compare(ctx context.Context, a, b Clause, fallback llmResult) (llmResult, error) {
	body := chatRequest{
		Model:       c.cfg.Model,
		Temperature: 0,
		Messages: []chatMessage{
			{
				Role: "system",
				Content: strings.Join([]string{
					"شما یک تحلیل‌گر تطبیق بانکی هستید.",
					"فقط JSON معتبر برگردانید.",
					"relationship_type باید یکی از این مقادیر باشد:",
					"full_contradiction, partial_contradiction, overlap_without_conflict, supersession, neutral.",
					"confidence عددی بین 0 و 1 باشد.",
					"rationale کوتاه و فارسی باشد.",
				}, "\n"),
			},
			{
				Role:    "user",
				Content: llmPrompt(a, b, fallback),
			},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return llmResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return llmResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return llmResult{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return llmResult{}, fmt.Errorf("llm status %d", res.StatusCode)
	}
	content, err := decodeChatContent(res.Body)
	if err != nil {
		return llmResult{}, err
	}
	return parseLLMResult(content)
}

// SummarizeCompliance یک خلاصه ساده و عملیاتی از مغایرت‌ها تولید می‌کند.
func (c *LLMClient) SummarizeCompliance(ctx context.Context, rels []Relationship) ([]string, error) {
	body := chatRequest{
		Model:       c.cfg.Model,
		Temperature: 0,
		Messages: []chatMessage{
			{
				Role: "system",
				Content: strings.Join([]string{
					"شما برای واحد حقوقی/تطبیق بانک گزارش می‌نویسید.",
					"فقط JSON معتبر برگردانید.",
					"خروجی باید این شکل باشد: {\"items\":[\"...\"]}.",
					"هر مورد ساده، کوتاه، فارسی و عملیاتی باشد.",
				}, "\n"),
			},
			{
				Role:    "user",
				Content: compliancePrompt(rels),
			},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("llm status %d", res.StatusCode)
	}
	content, err := decodeChatContent(res.Body)
	if err != nil {
		return nil, err
	}
	return parseComplianceSummary(content)
}

// DeepReviewRelationship یک رابطه را با LLM بررسی عمیق می‌کند.
func (c *LLMClient) DeepReviewRelationship(ctx context.Context, rel Relationship) (DeepReview, error) {
	body := chatRequest{
		Model:       c.cfg.Model,
		Temperature: 0,
		Messages: []chatMessage{
			{
				Role: "system",
				Content: strings.Join([]string{
					"شما کارشناس ارشد حقوقی/تطبیق بانکی هستید.",
					"دو بند بخشنامه را دقیق مقایسه کنید و برای کاربر غیر فنی توضیح دهید.",
					"اگر فقط هم‌پوشانی بدون مغایرت است، صریح بگویید چرا اقدام اصلاحی لازم نیست.",
					"فقط JSON معتبر برگردانید.",
					"verdict یکی از conflict, partial_conflict, compatible, supersession, needs_human_review باشد.",
					"severity یکی از critical, high, medium, low, none باشد.",
					"اگر نوع فعلی full_contradiction است verdict را conflict قرار دهید مگر اینکه نیازمند بررسی انسانی باشد.",
					"اگر نوع فعلی partial_contradiction است verdict را partial_conflict قرار دهید مگر اینکه نیازمند بررسی انسانی باشد.",
					"اگر نوع فعلی supersession است verdict را supersession قرار دهید مگر اینکه نیازمند بررسی انسانی باشد.",
					"برای مغایرت‌های فعلی، verdict compatible و severity none برنگردانید.",
				}, "\n"),
			},
			{Role: "user", Content: deepReviewPrompt(rel)},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return DeepReview{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return DeepReview{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return DeepReview{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return DeepReview{}, fmt.Errorf("llm status %d", res.StatusCode)
	}
	content, err := decodeChatContent(res.Body)
	if err != nil {
		return DeepReview{}, err
	}
	review, err := parseDeepReview(content)
	if err != nil {
		return DeepReview{}, err
	}
	review.GeneratedByLLM = true
	review.CreatedAt = time.Now().UTC()
	return review, nil
}

func llmPrompt(a, b Clause, fallback llmResult) string {
	return fmt.Sprintf(`دو بند بخشنامه را مقایسه کن.

بند اول:
شناسه: %s
موضوع: %s
نوع حکم: %s
شرایط استخراج‌شده: %v
متن: %s

بند دوم:
شناسه: %s
موضوع: %s
نوع حکم: %s
شرایط استخراج‌شده: %v
متن: %s

نتیجه قاعده‌محور اولیه:
relationship_type=%s
rationale=%s

فقط این JSON را برگردان:
{"relationship_type":"...","confidence":0.0,"rationale":"..."}`,
		a.ID, a.Subject, a.RulingType, a.ExtractedConditions, a.OriginalText,
		b.ID, b.Subject, b.RulingType, b.ExtractedConditions, b.OriginalText,
		fallback.RelationshipType, fallback.Rationale,
	)
}

func compliancePrompt(rels []Relationship) string {
	var b strings.Builder
	b.WriteString("این مغایرت‌ها را برای واحد حقوقی/تطبیق خلاصه کن. برای هر مورد بگو مشکل چیست و اقدام پیشنهادی چیست.\n")
	for i, rel := range rels {
		fmt.Fprintf(&b, "\nمورد %d:\nنوع: %s\nوضعیت حل: %s\nبند برنده: %s\nاقدام: %s\nدلیل: %s\nمتن اول: %s\nمتن دوم: %s\n",
			i+1,
			rel.RelationshipType,
			rel.ResolverStatus,
			rel.WinningClauseID,
			rel.RequiredAction,
			rel.Rationale,
			rel.Evidence["source_text"],
			rel.Evidence["target_text"],
		)
	}
	b.WriteString("\nفقط JSON با کلید items برگردان.")
	return b.String()
}

func deepReviewPrompt(rel Relationship) string {
	return fmt.Sprintf(`این رابطه را عمیق بررسی کن و پیام مبهم را به توضیح روشن تبدیل کن.

شناسه رابطه: %s
نوع فعلی: %s
اعتماد فعلی: %.2f
وضعیت حل فعلی: %s
بند برنده فعلی: %s
اقدام فعلی: %s
دلیل فعلی: %s

متن بند اول:
%s

متن بند دوم:
%s

خروجی فقط JSON:
{"verdict":"...","severity":"...","plain_explanation":"...","legal_reason":"...","recommended_action":"...","questions":["..."]}`,
		rel.ID,
		rel.RelationshipType,
		rel.Confidence,
		rel.ResolverStatus,
		rel.WinningClauseID,
		rel.RequiredAction,
		rel.Rationale,
		rel.Evidence["source_text"],
		rel.Evidence["target_text"],
	)
}

func parseLLMResult(content string) (llmResult, error) {
	content = cleanJSONContent(content)
	var out llmResult
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return llmResult{}, err
	}
	if !validRelationshipTypes[out.RelationshipType] {
		return llmResult{}, fmt.Errorf("invalid relationship_type %q", out.RelationshipType)
	}
	if out.Confidence < 0 || out.Confidence > 1 {
		return llmResult{}, fmt.Errorf("invalid confidence %v", out.Confidence)
	}
	if strings.TrimSpace(out.Rationale) == "" {
		return llmResult{}, errors.New("empty rationale")
	}
	return out, nil
}

func parseComplianceSummary(content string) ([]string, error) {
	content = cleanJSONContent(content)
	var out complianceSummaryResponse
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return nil, err
	}
	cleaned := make([]string, 0, len(out.Items))
	for _, item := range out.Items {
		item = strings.TrimSpace(item)
		if item != "" {
			cleaned = append(cleaned, item)
		}
	}
	if len(cleaned) == 0 {
		return nil, errors.New("empty compliance summary")
	}
	return cleaned, nil
}

func parseDeepReview(content string) (DeepReview, error) {
	content = cleanJSONContent(content)
	var out deepReviewResponse
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return DeepReview{}, err
	}
	if !validDeepVerdict(out.Verdict) {
		return DeepReview{}, fmt.Errorf("invalid verdict %q", out.Verdict)
	}
	if !validSeverity(out.Severity) {
		return DeepReview{}, fmt.Errorf("invalid severity %q", out.Severity)
	}
	if strings.TrimSpace(out.PlainExplanation) == "" || strings.TrimSpace(out.LegalReason) == "" || strings.TrimSpace(out.RecommendedAction) == "" {
		return DeepReview{}, errors.New("deep review missing explanation, legal reason, or action")
	}
	questions := make([]string, 0, len(out.Questions))
	for _, q := range out.Questions {
		if q = strings.TrimSpace(q); q != "" {
			questions = append(questions, q)
		}
	}
	return DeepReview{
		Verdict:           out.Verdict,
		Severity:          out.Severity,
		PlainExplanation:  strings.TrimSpace(out.PlainExplanation),
		LegalReason:       strings.TrimSpace(out.LegalReason),
		RecommendedAction: strings.TrimSpace(out.RecommendedAction),
		Questions:         questions,
	}, nil
}

func validDeepVerdict(v string) bool {
	switch v {
	case "conflict", "partial_conflict", "compatible", "supersession", "needs_human_review":
		return true
	default:
		return false
	}
}

func validSeverity(v string) bool {
	switch v {
	case "critical", "high", "medium", "low", "none":
		return true
	default:
		return false
	}
}

func cleanJSONContent(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content)
}

func decodeChatContent(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	raw := strings.TrimSpace(string(b))
	if strings.HasPrefix(raw, "data:") {
		return decodeSSEChatContent(raw)
	}
	var decoded chatResponse
	if err := json.Unmarshal(b, &decoded); err != nil {
		return "", err
	}
	if len(decoded.Choices) == 0 {
		return "", errors.New("llm returned no choices")
	}
	return decoded.Choices[0].Message.Content, nil
}

func decodeSSEChatContent(raw string) (string, error) {
	var out strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk chatResponse
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return "", err
		}
		for _, choice := range chunk.Choices {
			out.WriteString(choice.Delta.Content)
			if choice.Message.Content != "" {
				out.WriteString(choice.Message.Content)
			}
		}
	}
	if out.Len() == 0 {
		return "", errors.New("llm returned empty stream")
	}
	return out.String(), nil
}

func importantRelationships(rels []Relationship, limit int) []Relationship {
	out := make([]Relationship, 0, len(rels))
	for _, rel := range rels {
		if rel.RelationshipType == "overlap_without_conflict" || rel.RelationshipType == "neutral" {
			continue
		}
		out = append(out, rel)
		if limit > 0 && len(out) == limit {
			return out
		}
	}
	return out
}

func deterministicComplianceSummary(rels []Relationship) []string {
	out := make([]string, 0, len(rels))
	for _, rel := range rels {
		winner := rel.WinningClauseID
		if winner == "" {
			winner = "نیازمند تصمیم انسانی"
		}
		out = append(out, fmt.Sprintf("بین %s و %s مغایرت از نوع %s دیده شد؛ بند معتبر: %s؛ اقدام پیشنهادی: %s.",
			rel.SourceClauseID, rel.TargetClauseID, rel.RelationshipType, winner, rel.RequiredAction))
	}
	return out
}
