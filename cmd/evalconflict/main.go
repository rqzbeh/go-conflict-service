package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"conflict-service/internal/conflict"
)

type scenario struct {
	Name                string                     `json:"name"`
	Circulars           []conflict.CircularRequest `json:"circulars"`
	Expected            []expectedRelationship     `json:"expected"`
	ExpectedAbsentTypes []string                   `json:"expected_absent_types"`
	SeedDataDir         bool                       `json:"seed_data_dir"`
	AnalyzeID           string                     `json:"analyze_id"`
}

type expectedRelationship struct {
	SourceClauseID   string `json:"source_clause_id"`
	TargetClauseID   string `json:"target_clause_id"`
	RelationshipType string `json:"relationship_type"`
	WinningClauseID  string `json:"winning_clause_id"`
	ResolverStatus   string `json:"resolver_status"`
}

type scenarioResult struct {
	Name     string   `json:"name"`
	Expected int      `json:"expected"`
	Found    int      `json:"found"`
	Missed   []string `json:"missed,omitempty"`
	Extra    []string `json:"extra_actionable,omitempty"`
	AbsentOK bool     `json:"absent_ok,omitempty"`
	Pass     bool     `json:"pass"`
}

func main() {
	path := "eval/ground_truth.json"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	var scenarios []scenario
	if err := json.Unmarshal(b, &scenarios); err != nil {
		log.Fatal(err)
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		for _, c := range []string{
			"../EligibilityAssistant&IntelligentBankingOffer/data",
			"../../EligibilityAssistant&IntelligentBankingOffer/data",
		} {
			if st, err := os.Stat(c); err == nil && st.IsDir() {
				dataDir = c
				break
			}
		}
	}

	totalExpected, tp, fp := 0, 0, 0
	var details []scenarioResult
	for _, sc := range scenarios {
		store := conflict.NewStore()
		if sc.SeedDataDir {
			if dataDir == "" {
				log.Fatalf("scenario %s needs DATA_DIR", sc.Name)
			}
			if err := conflict.SeedFromDataDir(store, dataDir); err != nil {
				log.Fatalf("seed %s: %v", sc.Name, err)
			}
		}
		for _, req := range sc.Circulars {
			store.UpsertCircular(conflict.ParseCircular(req))
		}

		target := sc.AnalyzeID
		if target == "" {
			if len(sc.Circulars) == 0 {
				log.Fatalf("scenario %s has no circulars/analyze_id", sc.Name)
			}
			target = sc.Circulars[len(sc.Circulars)-1].ID
		}
		report, err := conflict.Analyze(store, target)
		if err != nil {
			log.Fatalf("%s: %v", sc.Name, err)
		}

		actionable := filterActionable(report.Relationships)
		sr := scenarioResult{Name: sc.Name, Expected: len(sc.Expected)}

		if len(sc.ExpectedAbsentTypes) > 0 {
			bad := false
			for _, rel := range actionable {
				if hasType(sc.ExpectedAbsentTypes, rel.RelationshipType) && involvesScenario(rel, sc) {
					bad = true
					sr.Extra = append(sr.Extra, fmt.Sprintf("%s %s__%s", rel.RelationshipType, rel.SourceClauseID, rel.TargetClauseID))
				}
			}
			sr.AbsentOK = !bad
			sr.Pass = !bad
			if bad {
				fp += len(sr.Extra)
			}
			details = append(details, sr)
			continue
		}

		matchedPred := map[string]bool{}
		for _, exp := range sc.Expected {
			totalExpected++
			if rel, ok := findExpected(actionable, exp); ok {
				tp++
				sr.Found++
				matchedPred[relKey(rel)] = true
			} else {
				sr.Missed = append(sr.Missed, fmt.Sprintf("%s %s__%s win=%s st=%s", exp.RelationshipType, exp.SourceClauseID, exp.TargetClauseID, exp.WinningClauseID, exp.ResolverStatus))
			}
		}
		// FP only among scenario circular pairs (not whole archive pollution)
		if !sc.SeedDataDir {
			for _, rel := range actionable {
				if !involvesScenario(rel, sc) {
					continue
				}
				if matchedPred[relKey(rel)] || isExpectedPair(sc.Expected, rel) {
					continue
				}
				fp++
				sr.Extra = append(sr.Extra, fmt.Sprintf("%s %s__%s", rel.RelationshipType, rel.SourceClauseID, rel.TargetClauseID))
			}
		}
		sr.Pass = len(sr.Missed) == 0 && len(sr.Extra) == 0
		details = append(details, sr)
	}

	fn := totalExpected - tp
	precision := ratio(tp, tp+fp)
	recall := ratio(tp, tp+fn)
	f1 := 0.0
	if precision+recall > 0 {
		f1 = 2 * precision * recall / (precision + recall)
	}
	out := map[string]any{
		"expected":       totalExpected,
		"true_positive":  tp,
		"false_positive": fp,
		"false_negative": fn,
		"precision":      round4(precision),
		"recall":         round4(recall),
		"f1":             round4(f1),
		"scenarios":      details,
		"correct":        tp,
		"predicted":      tp + fp,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		log.Fatal(err)
	}
	_ = os.MkdirAll("eval", 0o755)
	if raw, err := json.MarshalIndent(out, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join("eval", "conflict_metrics.json"), raw, 0o644)
	}
	if f1 < 1.0 || fn > 0 || fp > 0 {
		os.Exit(1)
	}
}

func filterActionable(rels []conflict.Relationship) []conflict.Relationship {
	var out []conflict.Relationship
	for _, r := range rels {
		if r.RelationshipType == "overlap_without_conflict" || r.RelationshipType == "neutral" {
			continue
		}
		out = append(out, r)
	}
	return out
}

func findExpected(rels []conflict.Relationship, exp expectedRelationship) (conflict.Relationship, bool) {
	for _, rel := range rels {
		if !samePair(rel.SourceClauseID, rel.TargetClauseID, exp.SourceClauseID, exp.TargetClauseID) {
			continue
		}
		if rel.RelationshipType != exp.RelationshipType {
			continue
		}
		if exp.WinningClauseID != "" && rel.WinningClauseID != exp.WinningClauseID {
			continue
		}
		if exp.ResolverStatus != "" && rel.ResolverStatus != exp.ResolverStatus {
			continue
		}
		return rel, true
	}
	return conflict.Relationship{}, false
}

func samePair(a1, a2, b1, b2 string) bool {
	return (a1 == b1 && a2 == b2) || (a1 == b2 && a2 == b1)
}

func isExpectedPair(exps []expectedRelationship, rel conflict.Relationship) bool {
	for _, exp := range exps {
		if samePair(rel.SourceClauseID, rel.TargetClauseID, exp.SourceClauseID, exp.TargetClauseID) {
			return true
		}
	}
	return false
}

func involvesScenario(rel conflict.Relationship, sc scenario) bool {
	ids := map[string]bool{}
	for _, c := range sc.Circulars {
		ids[c.ID] = true
	}
	srcCirc := strings.Split(rel.SourceClauseID, "#")[0]
	tgtCirc := strings.Split(rel.TargetClauseID, "#")[0]
	if len(ids) == 0 {
		return true
	}
	return ids[srcCirc] && ids[tgtCirc]
}

func hasType(types []string, t string) bool {
	for _, x := range types {
		if x == t {
			return true
		}
	}
	return false
}

func relKey(r conflict.Relationship) string {
	a, b := r.SourceClauseID, r.TargetClauseID
	if a > b {
		a, b = b, a
	}
	return a + "||" + b + "||" + r.RelationshipType
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func round4(v float64) float64 {
	return float64(int(v*10000+0.5)) / 10000
}
