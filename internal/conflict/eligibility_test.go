package conflict

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type eligibilityGroundTruth struct {
	Pairs []struct {
		NationalID string        `json:"national_id"`
		ProductID  string        `json:"product_id"`
		Decision   string        `json:"decision"`
		Evidence   []EvidenceRef `json:"evidence"`
	} `json:"pairs"`
}

func testAssistant(t *testing.T) (*Store, *EligibilityAssistant) {
	t.Helper()
	root := filepath.Join("..", "..", "..", "EligibilityAssistant&IntelligentBankingOffer")
	dataDir := filepath.Join(root, "data")
	store := NewStore()
	if err := SeedFromDataDir(store, dataDir); err != nil {
		t.Fatal(err)
	}
	assistant, err := LoadEligibilityAssistant(dataDir, root, store)
	if err != nil {
		t.Fatal(err)
	}
	return store, assistant
}

func TestAssistCoversRequiredScenarios(t *testing.T) {
	_, assistant := testAssistant(t)
	cases := []struct {
		name      string
		id        string
		productID string
		decision  string
		gapMetric string
	}{
		{"homemaker checkbook rejected with income/turnover gap", "12345678", "P01", "not_eligible", "monthly_income"},
		{"manager checkbook conditional", "23456789", "P01", "conditional", ""},
		{"employee checkbook close but turnover gap", "34567890", "P01", "not_eligible", "avg_monthly_turnover"},
		{"bounced cheque high risk rejects credit", "90123456", "P02", "not_eligible", "risk_level"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := assistant.Assist(AssistRequest{NationalID: tc.id})
			if err != nil {
				t.Fatal(err)
			}
			item := requireEligibility(t, got, tc.productID)
			if item.Decision != tc.decision {
				t.Fatalf("%s decision=%s, want %s; item=%+v", tc.productID, item.Decision, tc.decision, item)
			}
			if len(item.Evidence) == 0 {
				t.Fatalf("%s has no evidence", tc.productID)
			}
			if tc.gapMetric != "" && !hasGap(item, tc.gapMetric) {
				t.Fatalf("%s gaps=%+v, want metric %s", tc.productID, item.Gap, tc.gapMetric)
			}
			if got.Conversation == "" || got.LegalSummary == "" || got.PaymentImpact == "" {
				t.Fatalf("missing natural language outputs: %+v", got)
			}
		})
	}
}

func TestEligibilityMatchesAllOfficialGroundTruthPairs(t *testing.T) {
	_, assistant := testAssistant(t)
	path := filepath.Join("..", "..", "..", "EligibilityAssistant&IntelligentBankingOffer", "eval", "ground_truth.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var truth eligibilityGroundTruth
	if err := json.Unmarshal(b, &truth); err != nil {
		t.Fatal(err)
	}
	responses := map[string]AssistResponse{}
	for _, expected := range truth.Pairs {
		response, ok := responses[expected.NationalID]
		if !ok {
			response, err = assistant.Assist(AssistRequest{NationalID: expected.NationalID})
			if err != nil {
				t.Fatalf("assist %s: %v", expected.NationalID, err)
			}
			responses[expected.NationalID] = response
		}
		item := requireEligibility(t, response, expected.ProductID)
		if item.Decision != expected.Decision {
			t.Errorf("%s/%s decision=%s, want %s", expected.NationalID, expected.ProductID, item.Decision, expected.Decision)
		}
		for _, evidence := range expected.Evidence {
			if !containsEvidence(item.Evidence, evidence) {
				t.Errorf("%s/%s evidence=%+v, missing %+v", expected.NationalID, expected.ProductID, item.Evidence, evidence)
			}
		}
	}
}

func TestAssistNewCustomerIntakeAndColdStart(t *testing.T) {
	_, assistant := testAssistant(t)
	first, err := assistant.Assist(AssistRequest{NationalID: "99999991"})
	if err != nil {
		t.Fatal(err)
	}
	if first.CustomerStatus != "new" || first.Intake == nil || first.Intake.Field != "name" {
		t.Fatalf("first intake=%+v", first)
	}
	done, err := assistant.Assist(AssistRequest{
		NationalID: "99999991",
		SelfDeclared: map[string]any{
			"name":                    "مشتری جدید",
			"age":                     28,
			"job_category":            "student",
			"purpose_category":        "small_credit",
			"declared_monthly_income": 12_000_000,
			"requested_amount":        80_000_000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if done.Risk == nil || done.Risk.Mode != "cold_start" || done.Risk.RiskScore != 58 || done.Risk.RiskLevel != "medium" {
		t.Fatalf("risk=%+v, want cold_start medium score 58", done.Risk)
	}
	if requireEligibility(t, done, "P03").Decision == "eligible" {
		t.Fatalf("new customer consumer loan must not be directly eligible")
	}
	if done.LegalSummary == "" {
		t.Fatal("missing legal summary")
	}
}

func TestEligibilityHTTPContractAndMockAPIs(t *testing.T) {
	store, assistant := testAssistant(t)
	handler := NewServerWithEligibility(store, assistant)

	rec := doJSON(handler, http.MethodPost, "/assist", `{"national_id":"34567890"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("assist status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got AssistResponse
	mustDecode(t, rec, &got)
	if got.NationalID != "34567890" || len(got.Eligibility) != 6 || len(got.Offers) == 0 {
		t.Fatalf("assist response=%+v", got)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/identity/34567890", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("identity status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(handler, http.MethodPost, "/rbci/score", `{"mode":"lookup","national_id":"90123456"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rbci status=%d body=%s", rec.Code, rec.Body.String())
	}
	var risk RiskProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &risk); err != nil {
		t.Fatal(err)
	}
	if risk.RiskLevel != "high" {
		t.Fatalf("risk=%+v, want high", risk)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/eligibility/rules", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "BX-1001") {
		t.Fatalf("rules status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func requireEligibility(t *testing.T, res AssistResponse, productID string) EligibilityItem {
	t.Helper()
	for _, item := range res.Eligibility {
		if item.ProductID == productID {
			return item
		}
	}
	t.Fatalf("product %s not found in %+v", productID, res.Eligibility)
	return EligibilityItem{}
}

func hasGap(item EligibilityItem, metric string) bool {
	for _, gap := range item.Gap {
		if gap.Metric == metric {
			return true
		}
	}
	return false
}

func containsEvidence(items []EvidenceRef, expected EvidenceRef) bool {
	for _, item := range items {
		if item == expected {
			return true
		}
	}
	return false
}


func TestRiskContractAliasesAndRulesFromCircularText(t *testing.T) {
	store, assistant := testAssistant(t)
	handler := NewServerWithEligibility(store, assistant)

	// existing customer risk aliases on /assist
	rec := doJSON(handler, http.MethodPost, "/assist", `{"national_id":"34567890"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("assist status=%d body=%s", rec.Code, rec.Body.String())
	}
	var assist AssistResponse
	mustDecode(t, rec, &assist)
	if assist.Risk == nil || assist.Risk.Level == "" || assist.Risk.Score == 0 {
		t.Fatalf("missing OpenAPI risk aliases: %+v", assist.Risk)
	}
	if assist.Risk.Level != assist.Risk.RiskLevel || assist.Risk.Score != assist.Risk.RiskScore {
		t.Fatalf("risk aliases mismatch: %+v", assist.Risk)
	}

	// rules extracted from circular clause text
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/eligibility/rules", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("rules status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []ProductRule `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 6 {
		t.Fatalf("want 6 product rules, got %d", len(payload.Items))
	}
	foundP01 := false
	for _, rule := range payload.Items {
		if rule.ProductID != "P01" {
			continue
		}
		foundP01 = true
		if len(rule.Conditions) == 0 {
			t.Fatal("P01 has no conditions")
		}
		hasText := false
		for _, c := range rule.Conditions {
			if strings.TrimSpace(c.ClauseText) != "" {
				hasText = true
			}
			if c.Evidence.CircularID == "" || c.Evidence.Clause == "" {
				t.Fatalf("condition missing evidence: %+v", c)
			}
		}
		if !hasText {
			t.Fatal("P01 conditions lack clause_text from circulars")
		}
	}
	if !foundP01 {
		t.Fatal("P01 rule missing")
	}

	// cold-start aliases
	rec = doJSON(handler, http.MethodPost, "/rbci/score", `{"mode":"cold_start","self_declared":{"age":28,"job_category":"student","purpose_category":"small_credit","declared_monthly_income":12000000,"requested_amount":80000000}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("cold_start status=%d body=%s", rec.Code, rec.Body.String())
	}
	var risk RiskProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &risk); err != nil {
		t.Fatal(err)
	}
	if risk.RiskScore != 58 || risk.Score != 58 || risk.Level != "medium" || risk.RiskLevel != "medium" {
		t.Fatalf("cold-start aliases/score=%+v", risk)
	}
}
