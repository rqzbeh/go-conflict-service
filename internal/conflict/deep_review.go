package conflict

import (
	"fmt"
	"time"
)

func deterministicDeepReview(rel Relationship) DeepReview {
	verdict := "needs_human_review"
	severity := "medium"
	switch rel.RelationshipType {
	case "full_contradiction":
		verdict = "conflict"
		severity = "high"
	case "partial_contradiction":
		verdict = "partial_conflict"
		severity = "medium"
	case "supersession":
		verdict = "supersession"
		severity = "medium"
	case "overlap_without_conflict":
		verdict = "compatible"
		severity = "none"
	}
	winner := rel.WinningClauseID
	if winner == "" {
		winner = "نیازمند تصمیم انسانی"
	}
	return DeepReview{
		Verdict:           verdict,
		Severity:          severity,
		PlainExplanation:  fmt.Sprintf("رابطه %s بین %s و %s ثبت شده است. بند معتبر فعلی: %s.", rel.RelationshipType, rel.SourceClauseID, rel.TargetClauseID, winner),
		LegalReason:       rel.Rationale,
		RecommendedAction: rel.RequiredAction,
		GeneratedByLLM:    false,
		CreatedAt:         time.Now().UTC(),
	}
}

func chooseDeepReview(rel Relationship, got, fallback DeepReview) DeepReview {
	switch rel.RelationshipType {
	case "full_contradiction", "partial_contradiction", "supersession":
		if got.Verdict == "compatible" || got.Severity == "none" {
			return fallback
		}
	case "overlap_without_conflict":
		if got.Verdict == "conflict" || got.Verdict == "partial_conflict" {
			got.Verdict = "needs_human_review"
			if got.Severity == "none" {
				got.Severity = "low"
			}
		}
	}
	return got
}
