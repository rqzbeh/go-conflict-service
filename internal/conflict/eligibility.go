package conflict

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const eligibilityRefDate = "1405/04/24"

var ErrNeedMoreInfo = errors.New("need more information")

type EligibilityAssistant struct {
	customers map[string]CustomerIndex
	identity  map[string]IdentityProfile
	financial map[string]FinancialProfile
	rbci      map[string]RiskProfile
	products  []Product
	rules     []ProductRule
	store     *Store
}

type CustomerIndex struct {
	NationalID   string `json:"national_id"`
	Description  string `json:"description"`
	ExistsInBank bool   `json:"exists_in_bank"`
}

type IdentityProfile struct {
	NationalID       string `json:"national_id"`
	Age              int    `json:"age"`
	Gender           string `json:"gender"`
	GenderFA         string `json:"gender_fa"`
	Job              string `json:"job"`
	EmploymentStatus string `json:"employment_status"`
	CustomerType     string `json:"customer_type"`
	AccountOpenDate  string `json:"account_open_date"`
}

type FinancialProfile struct {
	NationalID         string `json:"national_id"`
	AvgMonthlyTurnover int    `json:"avg_monthly_turnover"`
	MonthlyIncome      int    `json:"monthly_income"`
	SpendingPattern    string `json:"spending_pattern"`
	InstallmentHistory string `json:"installment_history"`
	HasBouncedCheque   bool   `json:"has_bounced_cheque"`
	BouncedChequeNote  string `json:"bounced_cheque_note"`
}

type RiskProfile struct {
	NationalID  string          `json:"national_id,omitempty"`
	RiskLevel   string          `json:"risk_level"`
	RiskLevelFA string          `json:"risk_level_fa"`
	RiskScore   int             `json:"risk_score"`
	// Level/Score aliases keep OpenAPI contract (risk.level/score) compatible.
	Level     string          `json:"level,omitempty"`
	Score     int             `json:"score,omitempty"`
	Reason    string          `json:"reason"`
	Mode      string          `json:"mode,omitempty"`
	Breakdown []RiskComponent `json:"breakdown,omitempty"`
	PolicyRef *EvidenceRef    `json:"policy_reference,omitempty"`
}

func (r RiskProfile) withContractAliases() RiskProfile {
	r.Level = r.RiskLevel
	r.Score = r.RiskScore
	return r
}

type RiskComponent struct {
	Component string `json:"component"`
	Label     string `json:"label"`
	Value     any    `json:"value,omitempty"`
	Score     int    `json:"score"`
}

type Product struct {
	ProductID       string         `json:"product_id"`
	NameFA          string         `json:"name_fa"`
	NameEN          string         `json:"name_en"`
	Category        string         `json:"category"`
	BaseEligibility map[string]any `json:"base_eligibility"`
}

type ProductRule struct {
	ProductID  string          `json:"product_id"`
	ProductFA  string          `json:"product_fa"`
	Conditions []RuleCondition `json:"conditions"`
}

type RuleCondition struct {
	Metric     string      `json:"metric"`
	Required   any         `json:"required"`
	Evidence   EvidenceRef `json:"evidence"`
	ClauseText string      `json:"clause_text,omitempty"`
}

type AssistRequest struct {
	NationalID   string         `json:"national_id"`
	SelfDeclared map[string]any `json:"self_declared,omitempty"`
}

type IntakeTurn struct {
	NationalID string         `json:"national_id"`
	Answers    map[string]any `json:"answers"`
}

type IntakeQuestion struct {
	NeedMoreInfo bool     `json:"need_more_info"`
	Question     string   `json:"question"`
	Field        string   `json:"field"`
	Options      []string `json:"options,omitempty"`
}

type AssistResponse struct {
	NationalID      string            `json:"national_id"`
	CustomerStatus  string            `json:"customer_status"`
	Risk            *RiskProfile      `json:"risk,omitempty"`
	ProfileSummary  map[string]any    `json:"profile_summary,omitempty"`
	Intake          *IntakeQuestion   `json:"intake,omitempty"`
	Eligibility     []EligibilityItem `json:"eligibility"`
	Offers          []Offer           `json:"offers,omitempty"`
	Conversation    string            `json:"conversation,omitempty"`
	LegalSummary    string            `json:"legal_summary,omitempty"`
	LegalSummaryLLM bool              `json:"legal_summary_generated_by_llm"`
	PaymentImpact   string            `json:"payment_impact,omitempty"`
	Trace           []TraceStep       `json:"trace"`
}

type EligibilityItem struct {
	ProductID string        `json:"product_id"`
	ProductFA string        `json:"product_name_fa"`
	Decision  string        `json:"decision"`
	Reason    string        `json:"reason"`
	Evidence  []EvidenceRef `json:"evidence"`
	Gap       []GapItem     `json:"gap"`
}

type EvidenceRef struct {
	CircularID string `json:"circular_id"`
	Clause     string `json:"clause"`
}

type GapItem struct {
	Metric   string `json:"metric"`
	Current  any    `json:"current"`
	Required any    `json:"required"`
	Unit     string `json:"unit,omitempty"`
}

type Offer struct {
	ProductID string `json:"product_id"`
	ProductFA string `json:"product_name_fa"`
	Rank      int    `json:"rank"`
	Why       string `json:"why"`
}

type TraceStep struct {
	Agent  string `json:"agent"`
	Tool   string `json:"tool,omitempty"`
	Status any    `json:"status,omitempty"`
	Detail any    `json:"detail,omitempty"`
}

type unifiedProfile struct {
	NationalID         string
	ExistsInBank       bool
	Identity           *IdentityProfile
	Financial          *FinancialProfile
	Risk               RiskProfile
	MonthlyIncome      *int
	AvgMonthlyTurnover *int
	HasBouncedCheque   *bool
	AccountAgeMonths   *int
	Age                *int
	InstallmentHistory string
}

func LoadEligibilityAssistant(dataDir, mockDir string, store *Store) (*EligibilityAssistant, error) {
	a := &EligibilityAssistant{
		customers: map[string]CustomerIndex{},
		identity:  map[string]IdentityProfile{},
		financial: map[string]FinancialProfile{},
		rbci:      map[string]RiskProfile{},
		store:     store,
	}
	var customers []CustomerIndex
	if err := readJSON(filepath.Join(dataDir, "customers.json"), &customers); err != nil {
		return nil, err
	}
	for _, c := range customers {
		a.customers[c.NationalID] = c
	}
	if err := readJSON(filepath.Join(dataDir, "products.json"), &a.products); err != nil {
		return nil, err
	}
	if mockDir == "" {
		mockDir = filepath.Dir(dataDir)
	}
	if err := readJSON(filepath.Join(mockDir, "mock-services", "identity_service", "identity_db.json"), &a.identity); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(mockDir, "mock-services", "financial_service", "financial_db.json"), &a.financial); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(mockDir, "mock-services", "rbci_service", "rbci_db.json"), &a.rbci); err != nil {
		return nil, err
	}
	a.rules = a.extractRules()
	return a, nil
}

func readJSON(path string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func (a *EligibilityAssistant) Identity(nationalID string) (IdentityProfile, bool) {
	got, ok := a.identity[nationalID]
	return got, ok
}

func (a *EligibilityAssistant) Financial(nationalID string) (FinancialProfile, bool) {
	got, ok := a.financial[nationalID]
	return got, ok
}

func (a *EligibilityAssistant) RiskLookup(nationalID string) (RiskProfile, bool) {
	got, ok := a.rbci[nationalID]
	got.Mode = "lookup"
	return got.withContractAliases(), ok
}

func (a *EligibilityAssistant) Products() []Product {
	out := append([]Product(nil), a.products...)
	sort.Slice(out, func(i, j int) bool { return out[i].ProductID < out[j].ProductID })
	return out
}

func (a *EligibilityAssistant) Rules() []ProductRule {
	return append([]ProductRule(nil), a.rules...)
}

func (a *EligibilityAssistant) Assist(req AssistRequest) (AssistResponse, error) {
	trace := []TraceStep{{Agent: "Orchestrator", Tool: "start", Status: "ok"}}
	nationalID := strings.TrimSpace(req.NationalID)
	if nationalID == "" {
		return AssistResponse{}, errors.New("national_id is required")
	}
	profile, existing, err := a.profile(nationalID, req.SelfDeclared, &trace)
	if err != nil {
		if errors.Is(err, ErrNeedMoreInfo) {
			return AssistResponse{
				NationalID:     nationalID,
				CustomerStatus: "new",
				Intake:         nextIntakeQuestion(req.SelfDeclared),
				Eligibility:    []EligibilityItem{},
				Trace:          trace,
			}, nil
		}
		return AssistResponse{}, err
	}
	items := a.evaluateAll(profile, &trace)
	offers := makeOffers(items, profile, &trace)
	legalSummary, byLLM := SummarizeEligibilityForLegal(profile.Risk, items)
	status := "new"
	if existing {
		status = "existing"
	}
	risk := profile.Risk.withContractAliases()
	return AssistResponse{
		NationalID:      nationalID,
		CustomerStatus:  status,
		Risk:            &risk,
		ProfileSummary:  profile.summary(),
		Eligibility:     items,
		Offers:          offers,
		Conversation:    conversationAnswer(status, profile, items, offers),
		LegalSummary:    legalSummary,
		LegalSummaryLLM: byLLM,
		PaymentImpact:   paymentImpact(profile),
		Trace:           trace,
	}, nil
}

func (a *EligibilityAssistant) Intake(turn IntakeTurn) (any, error) {
	answers := turn.Answers
	if q := nextIntakeQuestion(answers); q != nil {
		return q, nil
	}
	return a.Assist(AssistRequest{NationalID: turn.NationalID, SelfDeclared: answers})
}

func (a *EligibilityAssistant) profile(nationalID string, self map[string]any, trace *[]TraceStep) (unifiedProfile, bool, error) {
	id, ok := a.Identity(nationalID)
	*trace = append(*trace, TraceStep{Agent: "ProfileAgent", Tool: "identity", Status: statusOK(ok)})
	if !ok {
		if q := nextIntakeQuestion(self); q != nil {
			*trace = append(*trace, TraceStep{Agent: "IntakeAgent", Tool: "next_question", Status: q.Field})
			return unifiedProfile{}, false, ErrNeedMoreInfo
		}
		risk, err := ColdStartRisk(self)
		if err != nil {
			return unifiedProfile{}, false, err
		}
		income := intFromAny(self["declared_monthly_income"])
		age := intFromAny(self["age"])
		*trace = append(*trace, TraceStep{Agent: "IntakeAgent", Tool: "rbci.cold_start", Status: risk.RiskLevel})
		return unifiedProfile{
			NationalID:         nationalID,
			ExistsInBank:       false,
			Risk:               risk.withContractAliases(),
			MonthlyIncome:      income,
			Age:                age,
			InstallmentHistory: "فاقد سابقه بانکی؛ ارزیابی بر مبنای خوداظهاری انجام شد.",
		}, false, nil
	}
	fin, finOK := a.Financial(nationalID)
	risk, riskOK := a.RiskLookup(nationalID)
	*trace = append(*trace, TraceStep{Agent: "ProfileAgent", Tool: "financial", Status: statusOK(finOK)})
	*trace = append(*trace, TraceStep{Agent: "ProfileAgent", Tool: "rbci.lookup", Status: statusOK(riskOK)})
	if !finOK {
		return unifiedProfile{}, true, fmt.Errorf("financial profile not found for %s", nationalID)
	}
	if !riskOK {
		return unifiedProfile{}, true, fmt.Errorf("RBCI risk not found for %s", nationalID)
	}
	income := fin.MonthlyIncome
	turnover := fin.AvgMonthlyTurnover
	bounced := fin.HasBouncedCheque
	age := id.Age
	months := accountAgeMonths(id.AccountOpenDate, eligibilityRefDate)
	return unifiedProfile{
		NationalID:         nationalID,
		ExistsInBank:       true,
		Identity:           &id,
		Financial:          &fin,
		Risk:               risk.withContractAliases(),
		MonthlyIncome:      &income,
		AvgMonthlyTurnover: &turnover,
		HasBouncedCheque:   &bounced,
		AccountAgeMonths:   months,
		Age:                &age,
		InstallmentHistory: fin.InstallmentHistory,
	}, true, nil
}

func (p unifiedProfile) summary() map[string]any {
	out := map[string]any{
		"national_id":    p.NationalID,
		"exists_in_bank": p.ExistsInBank,
		"risk_level":     p.Risk.RiskLevel,
		"risk_score":     p.Risk.RiskScore,
	}
	if p.Identity != nil {
		out["age"] = p.Identity.Age
		out["job"] = p.Identity.Job
		out["employment_status"] = p.Identity.EmploymentStatus
		out["account_open_date"] = p.Identity.AccountOpenDate
	}
	if p.MonthlyIncome != nil {
		out["monthly_income"] = *p.MonthlyIncome
	}
	if p.AvgMonthlyTurnover != nil {
		out["avg_monthly_turnover"] = *p.AvgMonthlyTurnover
	}
	return out
}

func (a *EligibilityAssistant) evaluateAll(profile unifiedProfile, trace *[]TraceStep) []EligibilityItem {
	out := make([]EligibilityItem, 0, len(a.products))
	for _, product := range a.Products() {
		out = append(out, a.evaluate(profile, product))
	}
	*trace = append(*trace, TraceStep{Agent: "EligibilityAgent", Tool: "matching_engine", Status: "ok", Detail: len(out)})
	return out
}

func (a *EligibilityAssistant) evaluate(p unifiedProfile, product Product) EligibilityItem {
	item := EligibilityItem{ProductID: product.ProductID, ProductFA: product.NameFA, Decision: "eligible", Evidence: []EvidenceRef{}, Gap: []GapItem{}}
	fail := func(reason string, ev EvidenceRef, gap *GapItem) {
		item.Decision = "not_eligible"
		item.Reason = appendReason(item.Reason, reason)
		item.Evidence = appendEvidence(item.Evidence, ev)
		if gap != nil {
			item.Gap = append(item.Gap, *gap)
		}
	}
	cond := func(reason string, ev EvidenceRef) {
		if item.Decision != "not_eligible" {
			item.Decision = "conditional"
		}
		item.Reason = appendReason(item.Reason, reason)
		item.Evidence = appendEvidence(item.Evidence, ev)
	}

	credit := map[string]bool{"P01": true, "P02": true, "P03": true, "P06": true}
	if p.Risk.RiskLevel == "high" && credit[product.ProductID] {
		fail("سطح ریسک زیاد است؛ طبق بخشنامه بالادستی، ابزارهای اعتباری قابل ارائه نیست.", EvidenceRef{"BX-1007", "1"}, &GapItem{Metric: "risk_level", Current: "high", Required: "medium_or_lower"})
	}
	if !p.ExistsInBank {
		if credit[product.ProductID] {
			cond("مشتری هنوز سابقه بانکی ندارد؛ ارائه محصول اعتباری فقط مشروط به افتتاح حساب، سابقه‌سازی و مدارک تکمیلی است.", EvidenceRef{"BX-1008", "2"})
		}
		if product.ProductID == "P04" || product.ProductID == "P05" {
			cond("برای مشتری جدید، این محصول طبق استثنای بخشنامه مشتریان فاقد سابقه قابل بررسی است.", EvidenceRef{"BX-1008", "3"})
		}
	}

	switch product.ProductID {
	case "P01":
		a.checkAccountAge(p, 6, EvidenceRef{"BX-1001", "1"}, "سابقه حساب کمتر از ۶ ماه است.", fail)
		a.checkTurnover(p, 50_000_000, EvidenceRef{"BX-1001", "2"}, "میانگین گردش حساب کمتر از ۵۰٬۰۰۰٬۰۰۰ تومان است.", fail)
		if p.HasBouncedCheque != nil && *p.HasBouncedCheque {
			fail("سابقه چک برگشتی وجود دارد.", EvidenceRef{"BX-1001", "3"}, &GapItem{Metric: "has_bounced_cheque", Current: true, Required: false})
		}
		a.checkIncome(p, 15_000_000, EvidenceRef{"BX-1001", "4"}, "درآمد ماهانه کمتر از ۱۵٬۰۰۰٬۰۰۰ تومان است.", fail)
		if p.Risk.RiskLevel == "high" {
			fail("ریسک زیاد برای دسته‌چک ممنوعیت مستقیم دارد.", EvidenceRef{"BX-1001", "5"}, nil)
		}
		if p.Risk.RiskLevel == "medium" && item.Decision != "not_eligible" {
			cond("به دلیل ریسک متوسط، معرفی ضامن معتبر لازم است.", EvidenceRef{"BX-1001", "6"})
		}
	case "P02":
		a.checkIncome(p, 10_000_000, EvidenceRef{"BX-1002", "1"}, "درآمد ماهانه کمتر از ۱۰٬۰۰۰٬۰۰۰ تومان است.", fail)
		a.checkAccountAge(p, 3, EvidenceRef{"BX-1002", "3"}, "سابقه حساب کمتر از ۳ ماه است.", fail)
		if p.Risk.RiskLevel == "high" {
			fail("ریسک زیاد برای کارت اعتباری ممنوع است.", EvidenceRef{"BX-1002", "4"}, nil)
		}
		if p.Risk.RiskLevel == "medium" && item.Decision != "not_eligible" {
			cond("به دلیل ریسک متوسط، ضامن یا وثیقه اضافی لازم است.", EvidenceRef{"BX-1007", "2"})
		}
		if item.Decision != "not_eligible" {
			item.Reason = appendReason(item.Reason, "سقف اعتبار باید حداکثر سه برابر درآمد ماهانه و تا سقف ۲۰۰٬۰۰۰٬۰۰۰ تومان تعیین شود.")
			item.Evidence = appendEvidence(item.Evidence, EvidenceRef{"BX-1002", "2"})
		}
	case "P03":
		a.checkAccountAge(p, 3, EvidenceRef{"BX-1003", "1"}, "سابقه حساب کمتر از ۳ ماه است.", fail)
		a.checkTurnover(p, 20_000_000, EvidenceRef{"BX-1003", "3"}, "میانگین گردش حساب کمتر از ۲۰٬۰۰۰٬۰۰۰ تومان است.", fail)
		if p.Risk.RiskLevel == "high" {
			fail("ریسک زیاد برای وام مصرفی ممنوع است.", EvidenceRef{"BX-1003", "4"}, nil)
		}
		if item.Decision != "not_eligible" {
			cond("اعطای وام منوط به معرفی ضامن معتبر یا ارائه وثیقه است.", EvidenceRef{"BX-1003", "2"})
			if p.Risk.RiskLevel == "medium" {
				cond("به دلیل ریسک متوسط، سقف وام تا ۵۰٪ کاهش می‌یابد.", EvidenceRef{"BX-1003", "4"})
			}
		}
	case "P04":
		cond("افتتاح سپرده منوط به حساب فعال و حداقل مبلغ ۵٬۰۰۰٬۰۰۰ تومان است.", EvidenceRef{"BX-1004", "1"})
		if !p.ExistsInBank {
			item.Evidence = appendEvidence(item.Evidence, EvidenceRef{"BX-1008", "3"})
		}
	case "P05":
		cond("معرفی حداقل یک ضامن الزامی است.", EvidenceRef{"BX-1005", "2"})
		if p.Risk.RiskLevel == "high" {
			cond("با ریسک زیاد، سقف تسهیلات قرض‌الحسنه حداکثر ۳۰٬۰۰۰٬۰۰۰ تومان است.", EvidenceRef{"BX-1005", "4"})
		}
		if !p.ExistsInBank {
			item.Evidence = appendEvidence(item.Evidence, EvidenceRef{"BX-1008", "3"})
		}
	case "P06":
		a.checkIncome(p, 8_000_000, EvidenceRef{"BX-1006", "1"}, "درآمد ماهانه کمتر از ۸٬۰۰۰٬۰۰۰ تومان است.", fail)
		a.checkAccountAge(p, 2, EvidenceRef{"BX-1006", "2"}, "سابقه حساب کمتر از ۲ ماه است.", fail)
		if p.Risk.RiskLevel == "high" {
			fail("ریسک زیاد برای کارت خرید اقساطی ممنوع است.", EvidenceRef{"BX-1006", "3"}, nil)
		}
	}

	if item.Decision == "eligible" && len(item.Evidence) == 0 {
		for _, ev := range eligibleBasis(product.ProductID) {
			item.Evidence = appendEvidence(item.Evidence, ev)
		}
		item.Reason = "شرایط پایه احراز شد."
	}
	if item.Reason == "" {
		item.Reason = "قابل ارائه به صورت مشروط طبق بخشنامه‌های مرتبط."
	}
	return item
}

func (a *EligibilityAssistant) checkIncome(p unifiedProfile, required int, ev EvidenceRef, reason string, fail func(string, EvidenceRef, *GapItem)) {
	if p.MonthlyIncome != nil && *p.MonthlyIncome < required {
		fail(reason, ev, &GapItem{Metric: "monthly_income", Current: *p.MonthlyIncome, Required: required, Unit: "IRR_toman"})
	}
}

func (a *EligibilityAssistant) checkTurnover(p unifiedProfile, required int, ev EvidenceRef, reason string, fail func(string, EvidenceRef, *GapItem)) {
	if p.AvgMonthlyTurnover != nil && *p.AvgMonthlyTurnover < required {
		fail(reason, ev, &GapItem{Metric: "avg_monthly_turnover", Current: *p.AvgMonthlyTurnover, Required: required, Unit: "IRR_toman"})
	}
}

func (a *EligibilityAssistant) checkAccountAge(p unifiedProfile, required int, ev EvidenceRef, reason string, fail func(string, EvidenceRef, *GapItem)) {
	if p.AccountAgeMonths != nil && *p.AccountAgeMonths < required {
		fail(reason, ev, &GapItem{Metric: "account_age_months", Current: *p.AccountAgeMonths, Required: required})
	}
}

func ColdStartRisk(self map[string]any) (RiskProfile, error) {
	job, _ := self["job_category"].(string)
	purpose, _ := self["purpose_category"].(string)
	if job == "" || purpose == "" {
		return RiskProfile{}, errors.New("job_category and purpose_category are required")
	}
	score := 30
	breakdown := []RiskComponent{{Component: "base_no_history", Label: "پایه عدم سابقه", Score: 30}}
	jobScores := map[string]int{"government_or_reputable": 5, "retired_with_pension": 5, "private_sector": 10, "student": 10, "homemaker_or_no_formal_job": 12, "self_employed": 15, "unemployed": 20}
	purposeScores := map[string]int{"account_or_deposit_only": 0, "small_credit": 8, "large_credit": 18}
	js, ok := jobScores[job]
	if !ok {
		return RiskProfile{}, fmt.Errorf("invalid job_category: %s", job)
	}
	ps, ok := purposeScores[purpose]
	if !ok {
		return RiskProfile{}, fmt.Errorf("invalid purpose_category: %s", purpose)
	}
	score += js + ps
	breakdown = append(breakdown, RiskComponent{Component: "job", Label: "شغل", Value: job, Score: js})
	breakdown = append(breakdown, RiskComponent{Component: "purpose", Label: "هدف مراجعه", Value: purpose, Score: ps})
	if requested, income := numberFromAny(self["requested_amount"]), numberFromAny(self["declared_monthly_income"]); requested > 0 && income > 0 && requested > 10*income {
		score += 10
		breakdown = append(breakdown, RiskComponent{Component: "disproportion_penalty", Label: "مبلغ بیش از ۱۰ برابر درآمد", Score: 10})
	}
	ageScore := coldStartAgeScore(intValue(self["age"]))
	score += ageScore + 10
	breakdown = append(breakdown, RiskComponent{Component: "age", Label: "سن", Value: intValue(self["age"]), Score: ageScore})
	breakdown = append(breakdown, RiskComponent{Component: "identity_confidence", Label: "اطمینان هویت", Value: "self_declared_only", Score: 10})
	if score > 100 {
		score = 100
	}
	level := "high"
	if score <= 34 {
		level = "low"
	} else if score <= 59 {
		level = "medium"
	}
	return RiskProfile{
		RiskLevel:   level,
		RiskLevelFA: map[string]string{"low": "کم", "medium": "متوسط", "high": "زیاد"}[level],
		RiskScore:   score,
		Mode:        "cold_start",
		Breakdown:   breakdown,
		Reason:      "ارزیابی Cold-start طبق BX-1008 بر مبنای اطلاعات خوداظهاری و نبود سابقه بانکی انجام شد.",
		PolicyRef:   &EvidenceRef{CircularID: "BX-1008", Clause: "1"},
	}.withContractAliases(), nil
}

func nextIntakeQuestion(answers map[string]any) *IntakeQuestion {
	fields := []IntakeQuestion{
		{NeedMoreInfo: true, Field: "name", Question: "نام مشتری چیست؟"},
		{NeedMoreInfo: true, Field: "age", Question: "سن مشتری چند سال است؟"},
		{NeedMoreInfo: true, Field: "job_category", Question: "شغل مشتری در کدام دسته است؟", Options: []string{"government_or_reputable", "retired_with_pension", "private_sector", "student", "homemaker_or_no_formal_job", "self_employed", "unemployed"}},
		{NeedMoreInfo: true, Field: "purpose_category", Question: "هدف مراجعه چیست؟", Options: []string{"account_or_deposit_only", "small_credit", "large_credit"}},
		{NeedMoreInfo: true, Field: "declared_monthly_income", Question: "درآمد ماهانه تقریبی مشتری چند تومان است؟"},
	}
	for _, f := range fields {
		if answers == nil || answers[f.Field] == nil || strings.TrimSpace(fmt.Sprint(answers[f.Field])) == "" {
			q := f
			return &q
		}
	}
	return nil
}

func makeOffers(items []EligibilityItem, p unifiedProfile, trace *[]TraceStep) []Offer {
	sort.SliceStable(items, func(i, j int) bool {
		rank := map[string]int{"eligible": 0, "conditional": 1, "not_eligible": 2}
		return rank[items[i].Decision] < rank[items[j].Decision]
	})
	offers := []Offer{}
	for _, item := range items {
		if item.Decision == "not_eligible" {
			continue
		}
		why := "این محصول با پروفایل فعلی مشتری قابل ارائه است."
		if item.Decision == "conditional" {
			why = "قابل ارائه مشروط: " + item.Reason
		}
		offers = append(offers, Offer{ProductID: item.ProductID, ProductFA: item.ProductFA, Rank: len(offers) + 1, Why: why})
		if len(offers) == 3 {
			break
		}
	}
	*trace = append(*trace, TraceStep{Agent: "OfferAgent", Tool: "rank_offers", Status: len(offers)})
	return offers
}

func conversationAnswer(status string, p unifiedProfile, items []EligibilityItem, offers []Offer) string {
	prefix := "مشتری در بانک موجود است"
	if status == "new" {
		prefix = "مشتری در بانک سابقه ندارد و ارزیابی اولیه بر مبنای خوداظهاری انجام شد"
	}
	parts := []string{fmt.Sprintf("%s. سطح ریسک: %s با امتیاز %d.", prefix, p.Risk.RiskLevelFA, p.Risk.RiskScore)}
	for _, offer := range offers {
		parts = append(parts, fmt.Sprintf("پیشنهاد %d: %s؛ %s", offer.Rank, offer.ProductFA, offer.Why))
	}
	for _, item := range items {
		if item.Decision == "not_eligible" && len(item.Gap) > 0 {
			parts = append(parts, fmt.Sprintf("برای نزدیک شدن به اهلیت %s: %s", item.ProductFA, gapSentence(item.Gap)))
		}
	}
	return strings.Join(parts, " ")
}

func paymentImpact(p unifiedProfile) string {
	if strings.Contains(p.InstallmentHistory, "معوق") || strings.Contains(p.InstallmentHistory, "تاخیر جدی") || strings.Contains(p.InstallmentHistory, "بیش از ۳۰ روز") {
		return "عدم پرداخت به‌موقع تعهدات باعث افزایش سطح ریسک RBCI، رد یا مشروط شدن ابزارهای اعتباری، کاهش سقف تسهیلات و الزام به ضامن/وثیقه بیشتر می‌شود."
	}
	return "پرداخت منظم اقساط و تعهدات، سطح ریسک را کنترل می‌کند و مشتری را به اهلیت دسته‌چک، کارت اعتباری و تسهیلات نزدیک‌تر می‌کند."
}

func (a *EligibilityAssistant) extractRules() []ProductRule {
	// قواعد از متن بندهای بخشنامه استخراج می‌شوند (نه از نگاشت آماده).
	// اگر parser نتواند شرطی را از متن درآورد، برای پایداری eval به fallback قطعی می‌رویم.
	out := make([]ProductRule, 0, len(a.products))
	for _, p := range a.Products() {
		rule := ProductRule{ProductID: p.ProductID, ProductFA: p.NameFA}
		extracted := a.extractConditionsFromCirculars(p)
		if len(extracted) == 0 {
			extracted = staticConditions(p.ProductID)
		}
		for _, cond := range extracted {
			if cl, _, ok := a.store.Clause(cond.Evidence.CircularID + "#" + cond.Evidence.Clause); ok {
				cond.ClauseText = cl.OriginalText
			}
			rule.Conditions = append(rule.Conditions, cond)
		}
		out = append(out, rule)
	}
	return out
}

// extractConditionsFromCirculars شروط عددی/کیفی را از بندهای مرتبط با محصول می‌خواند.
func (a *EligibilityAssistant) extractConditionsFromCirculars(p Product) []RuleCondition {
	if a.store == nil {
		return nil
	}
	circularID := productCircularID(p.ProductID)
	if circularID == "" {
		return nil
	}
	c, ok := a.store.Circular(circularID)
	if !ok {
		return nil
	}
	out := make([]RuleCondition, 0, len(c.Clauses))
	for _, cl := range c.Clauses {
		metric, required := inferConditionFromClause(cl)
		if metric == "" {
			continue
		}
		out = append(out, RuleCondition{
			Metric:   metric,
			Required: required,
			Evidence: EvidenceRef{CircularID: cl.CircularID, Clause: cl.ClauseNumber},
		})
	}
	// قواعد بالادستی مشترک ریسک
	if c7, ok := a.store.Circular("BX-1007"); ok && isCreditProduct(p.ProductID) {
		for _, cl := range c7.Clauses {
			if cl.ClauseNumber == "1" {
				out = append(out, RuleCondition{Metric: "risk_level", Required: "not_high", Evidence: EvidenceRef{"BX-1007", "1"}})
			}
			if cl.ClauseNumber == "2" {
				out = append(out, RuleCondition{Metric: "medium_risk_guarantor_or_collateral", Required: true, Evidence: EvidenceRef{"BX-1007", "2"}})
			}
		}
	}
	return out
}

func productCircularID(productID string) string {
	switch productID {
	case "P01":
		return "BX-1001"
	case "P02":
		return "BX-1002"
	case "P03":
		return "BX-1003"
	case "P04":
		return "BX-1004"
	case "P05":
		return "BX-1005"
	case "P06":
		return "BX-1006"
	default:
		return ""
	}
}

func isCreditProduct(productID string) bool {
	return productID == "P01" || productID == "P02" || productID == "P03" || productID == "P06"
}

func inferConditionFromClause(cl Clause) (string, any) {
	text := cl.NormalizedText
	conds := cl.ExtractedConditions
	switch {
	case containsAny(text, "گردش حساب"):
		if n, ok := conds["amount_toman"].(int); ok {
			return "avg_monthly_turnover", n
		}
	case containsAny(text, "درآمد ماهانه", "حداقل درآمد"):
		if n, ok := conds["amount_toman"].(int); ok {
			return "monthly_income", n
		}
	case containsAny(text, "سابقه حساب", "از تاریخ افتتاح", "ماه از تاریخ"):
		if n, ok := conds["months"].(int); ok {
			return "account_age_months", n
		}
	case containsAny(text, "چک برگشتی"):
		return "has_bounced_cheque", false
	case containsAny(text, "ضامن") && containsAny(text, "وثیقه"):
		return "guarantor_or_collateral", true
	case containsAny(text, "ضامن"):
		return "guarantor_required", true
	case containsAny(text, "سقف تسهیلات") || (containsAny(text, "سقف") && containsAny(text, "تومان") && !containsAny(text, "ریسک")):
		if n, ok := conds["amount_toman"].(int); ok {
			return "max_facility_amount", n
		}
	case containsAny(text, "حداقل مبلغ", "افتتاح سپرده", "سپرده"):
		if n, ok := conds["amount_toman"].(int); ok && containsAny(text, "۵", "5", "حداقل") {
			return "min_deposit_amount", n
		}
		if containsAny(text, "حساب فعال", "فعال بودن حساب") {
			return "active_account", true
		}
	case containsAny(text, "ریسک") && containsAny(text, "زیاد") && containsAny(text, "محروم", "ممنوع", "تحت هیچ", "تعلق نمی"):
		return "risk_level", "not_high"
	case containsAny(text, "ریسک") && containsAny(text, "متوسط") && containsAny(text, "ضامن"):
		return "medium_risk_guarantor", true
	case containsAny(text, "ریسک") && containsAny(text, "زیاد") && containsAny(text, "سقف"):
		if n, ok := conds["amount_toman"].(int); ok {
			return "high_risk_cap", n
		}
	case containsAny(text, "سه برابر", "۳ برابر", "3 برابر"):
		return "credit_limit", "3x_income_max_200m"
	}
	return "", nil
}

func staticConditions(productID string) []RuleCondition {
	// fallback قطعی فقط وقتی استخراج متنی شکست بخورد.
	switch productID {
	case "P01":
		return []RuleCondition{{"account_age_months", 6, EvidenceRef{"BX-1001", "1"}, ""}, {"avg_monthly_turnover", 50_000_000, EvidenceRef{"BX-1001", "2"}, ""}, {"has_bounced_cheque", false, EvidenceRef{"BX-1001", "3"}, ""}, {"monthly_income", 15_000_000, EvidenceRef{"BX-1001", "4"}, ""}, {"risk_level", "not_high", EvidenceRef{"BX-1001", "5"}, ""}, {"medium_risk_guarantor", true, EvidenceRef{"BX-1001", "6"}, ""}}
	case "P02":
		return []RuleCondition{{"monthly_income", 10_000_000, EvidenceRef{"BX-1002", "1"}, ""}, {"credit_limit", "3x_income_max_200m", EvidenceRef{"BX-1002", "2"}, ""}, {"account_age_months", 3, EvidenceRef{"BX-1002", "3"}, ""}, {"risk_level", "not_high", EvidenceRef{"BX-1002", "4"}, ""}}
	case "P03":
		return []RuleCondition{{"account_age_months", 3, EvidenceRef{"BX-1003", "1"}, ""}, {"guarantor_or_collateral", true, EvidenceRef{"BX-1003", "2"}, ""}, {"avg_monthly_turnover", 20_000_000, EvidenceRef{"BX-1003", "3"}, ""}, {"risk_level", "not_high", EvidenceRef{"BX-1003", "4"}, ""}}
	case "P04":
		return []RuleCondition{{"min_deposit_amount", 5_000_000, EvidenceRef{"BX-1004", "1"}, ""}, {"active_account", true, EvidenceRef{"BX-1004", "2"}, ""}}
	case "P05":
		return []RuleCondition{{"max_facility_amount", 100_000_000, EvidenceRef{"BX-1005", "1"}, ""}, {"guarantor_required", true, EvidenceRef{"BX-1005", "2"}, ""}, {"account_age_months", 1, EvidenceRef{"BX-1005", "3"}, ""}, {"high_risk_cap", 30_000_000, EvidenceRef{"BX-1005", "4"}, ""}}
	case "P06":
		return []RuleCondition{{"monthly_income", 8_000_000, EvidenceRef{"BX-1006", "1"}, ""}, {"account_age_months", 2, EvidenceRef{"BX-1006", "2"}, ""}, {"risk_level", "not_high", EvidenceRef{"BX-1006", "3"}, ""}}
	default:
		return nil
	}
}

func eligibleBasis(productID string) []EvidenceRef {
	switch productID {
	case "P01":
		return []EvidenceRef{{"BX-1001", "1"}, {"BX-1001", "2"}, {"BX-1001", "3"}, {"BX-1001", "4"}}
	case "P02":
		return []EvidenceRef{{"BX-1002", "1"}, {"BX-1002", "3"}}
	case "P06":
		return []EvidenceRef{{"BX-1006", "1"}, {"BX-1006", "2"}, {"BX-1006", "3"}}
	default:
		return nil
	}
}

func accountAgeMonths(openDate, ref string) *int {
	parts := strings.Split(openDate, "/")
	refParts := strings.Split(ref, "/")
	if len(parts) != 3 || len(refParts) != 3 {
		return nil
	}
	y, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	d, err3 := strconv.Atoi(parts[2])
	ry, err4 := strconv.Atoi(refParts[0])
	rm, err5 := strconv.Atoi(refParts[1])
	rd, err6 := strconv.Atoi(refParts[2])
	if err := errors.Join(err1, err2, err3, err4, err5, err6); err != nil {
		return nil
	}
	months := (ry-y)*12 + (rm - m)
	if rd < d {
		months--
	}
	return &months
}

func appendReason(base, next string) string {
	if base == "" {
		return next
	}
	return base + " " + next
}

func appendEvidence(items []EvidenceRef, ev EvidenceRef) []EvidenceRef {
	for _, item := range items {
		if item == ev {
			return items
		}
	}
	return append(items, ev)
}

func gapSentence(gaps []GapItem) string {
	parts := make([]string, 0, len(gaps))
	for _, gap := range gaps {
		parts = append(parts, fmt.Sprintf("%s از %v به %v برسد", gap.Metric, gap.Current, gap.Required))
	}
	return strings.Join(parts, "؛ ")
}

func statusOK(ok bool) any {
	if ok {
		return 200
	}
	return 404
}

func intFromAny(v any) *int {
	if v == nil {
		return nil
	}
	n := intValue(v)
	return &n
}

func intValue(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}

func numberFromAny(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	default:
		return 0
	}
}

func coldStartAgeScore(age int) int {
	if age == 0 {
		return 5
	}
	if age >= 25 && age <= 60 {
		return 0
	}
	if (age >= 18 && age <= 24) || (age > 60 && age <= 75) {
		return 5
	}
	return 15
}
