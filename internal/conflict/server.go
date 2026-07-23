package conflict

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Server هندلرهای HTTP سرویس تشخیص مغایرت را نگه می‌دارد.
type Server struct {
	store       *Store
	eligibility *EligibilityAssistant
}

// NewServer مسیرهای API و UI را روی یک ServeMux ثبت می‌کند.
func NewServer(store *Store) http.Handler {
	return NewServerWithEligibility(store, nil)
}

func NewServerWithEligibility(store *Store, eligibility *EligibilityAssistant) http.Handler {
	s := &Server{store: store, eligibility: eligibility}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.home)
	mux.Handle("GET /assets/", uiFileServer())
	mux.Handle("GET /manifest.webmanifest", uiFileServer())
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /assist", s.assist)
	mux.HandleFunc("POST /assist/intake", s.assistIntake)
	mux.HandleFunc("GET /identity/", s.identity)
	mux.HandleFunc("GET /financial/", s.financial)
	mux.HandleFunc("POST /rbci/score", s.rbciScore)
	mux.HandleFunc("GET /products", s.products)
	mux.HandleFunc("GET /eligibility/rules", s.eligibilityRules)
	mux.HandleFunc("GET /circulars", s.listCirculars)
	mux.HandleFunc("POST /circulars", s.createCircular)
	mux.HandleFunc("GET /search", s.search)
	mux.HandleFunc("GET /circulars/", s.circularSubroutes)
	mux.HandleFunc("POST /circulars/", s.circularSubroutes)
	mux.HandleFunc("PUT /circulars/", s.circularSubroutes)
	mux.HandleFunc("DELETE /circulars/", s.circularSubroutes)
	mux.HandleFunc("POST /scans/archive", s.scanArchive)
	mux.HandleFunc("GET /relationships", s.listRelationships)
	mux.HandleFunc("GET /relationships/", s.getRelationship)
	mux.HandleFunc("POST /relationships/", s.relationshipSubroutes)
	mux.HandleFunc("PATCH /relationships/", s.reviewRelationship)
	return mux
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	uiFileServer().ServeHTTP(w, r)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"service":   "go-conflict-service",
		"circulars": s.store.CircularCount(),
		"llm":       LLMRuntimeStatus(),
		"persistence": map[string]any{
			"enabled": s.store.Persistent(),
		},
		"eligibility": map[string]any{
			"enabled": s.eligibility != nil,
		},
	})
}

func (s *Server) assist(w http.ResponseWriter, r *http.Request) {
	if s.eligibility == nil {
		writeError(w, http.StatusServiceUnavailable, "eligibility_disabled", "eligibility assistant is not configured")
		return
	}
	var req AssistRequest
	if err := decodeStrictJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := s.eligibility.Assist(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "assist_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) assistIntake(w http.ResponseWriter, r *http.Request) {
	if s.eligibility == nil {
		writeError(w, http.StatusServiceUnavailable, "eligibility_disabled", "eligibility assistant is not configured")
		return
	}
	var req IntakeTurn
	if err := decodeStrictJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := s.eligibility.Intake(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "intake_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) identity(w http.ResponseWriter, r *http.Request) {
	if s.eligibility == nil {
		writeError(w, http.StatusServiceUnavailable, "eligibility_disabled", "eligibility assistant is not configured")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/identity/")
	if got, ok := s.eligibility.Identity(id); ok {
		writeJSON(w, http.StatusOK, got)
		return
	}
	writeError(w, http.StatusNotFound, "identity_not_found", "customer identity not found")
}

func (s *Server) financial(w http.ResponseWriter, r *http.Request) {
	if s.eligibility == nil {
		writeError(w, http.StatusServiceUnavailable, "eligibility_disabled", "eligibility assistant is not configured")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/financial/")
	if got, ok := s.eligibility.Financial(id); ok {
		writeJSON(w, http.StatusOK, got)
		return
	}
	writeError(w, http.StatusNotFound, "financial_not_found", "customer financial profile not found")
}

func (s *Server) rbciScore(w http.ResponseWriter, r *http.Request) {
	if s.eligibility == nil {
		writeError(w, http.StatusServiceUnavailable, "eligibility_disabled", "eligibility assistant is not configured")
		return
	}
	var req struct {
		Mode         string         `json:"mode"`
		NationalID   string         `json:"national_id"`
		SelfDeclared map[string]any `json:"self_declared"`
	}
	if err := decodeStrictJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.Mode == "cold_start" {
		got, err := ColdStartRisk(req.SelfDeclared)
		if err != nil {
			writeError(w, http.StatusBadRequest, "cold_start_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, got)
		return
	}
	if req.Mode == "lookup" {
		if got, ok := s.eligibility.RiskLookup(req.NationalID); ok {
			writeJSON(w, http.StatusOK, got)
			return
		}
		writeError(w, http.StatusNotFound, "risk_not_found", "customer risk profile not found")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_mode", "mode must be lookup or cold_start")
}

func (s *Server) products(w http.ResponseWriter, r *http.Request) {
	if s.eligibility == nil {
		writeError(w, http.StatusServiceUnavailable, "eligibility_disabled", "eligibility assistant is not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.eligibility.Products()})
}

func (s *Server) eligibilityRules(w http.ResponseWriter, r *http.Request) {
	if s.eligibility == nil {
		writeError(w, http.StatusServiceUnavailable, "eligibility_disabled", "eligibility assistant is not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.eligibility.Rules()})
}

func (s *Server) listCirculars(w http.ResponseWriter, r *http.Request) {
	q := NormalizePersian(r.URL.Query().Get("q"))
	items := s.store.ListCirculars()
	if q != "" {
		filtered := items[:0]
		for _, c := range items {
			if strings.Contains(c.NormalizedText, q) || strings.Contains(NormalizePersian(c.Title), q) {
				filtered = append(filtered, c)
			}
		}
		items = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(items), "items": items})
}

func (s *Server) createCircular(w http.ResponseWriter, r *http.Request) {
	var req CircularRequest
	if err := decodeStrictJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "missing_text", "text is required")
		return
	}
	if err := validateCircularRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_issue_date", err.Error())
		return
	}
	if req.ID == "" {
		req.ID = "C-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
	} else if s.store.HasCircular(req.ID) {
		writeError(w, http.StatusConflict, "circular_exists", "circular id already exists; use PUT to update")
		return
	}
	c := s.store.UpsertCircular(ParseCircular(req))
	writeJSON(w, http.StatusCreated, map[string]any{
		"circular_id":  c.ID,
		"clause_count": len(c.Clauses),
		"status":       c.Status,
	})
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		writeError(w, http.StatusBadRequest, "missing_query", "q is required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"query": q, "items": SearchClauses(s.store, q, 20)})
}

func (s *Server) circularSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/circulars/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && r.Method == http.MethodGet {
		s.getCircular(w, r, parts[0])
		return
	}
	if len(parts) == 1 && r.Method == http.MethodPut {
		s.updateCircular(w, r, parts[0])
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		s.deleteCircular(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "analyze" && r.Method == http.MethodPost {
		s.analyzeCircular(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "report" && r.Method == http.MethodGet {
		s.getReport(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "clauses" && r.Method == http.MethodGet {
		s.getClause(w, r, parts[0], parts[2])
		return
	}
	if len(parts) == 3 && parts[1] == "clauses" && r.Method == http.MethodPut {
		s.updateClause(w, r, parts[0], parts[2])
		return
	}
	http.NotFound(w, r)
}

func (s *Server) getCircular(w http.ResponseWriter, r *http.Request, id string) {
	c, ok := s.store.Circular(id)
	if !ok {
		writeError(w, http.StatusNotFound, "circular_not_found", "circular not found")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) updateCircular(w http.ResponseWriter, r *http.Request, id string) {
	if !s.store.HasCircular(id) {
		writeError(w, http.StatusNotFound, "circular_not_found", "circular not found")
		return
	}
	var req CircularRequest
	if err := decodeStrictJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "missing_text", "text is required")
		return
	}
	if err := validateCircularRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_issue_date", err.Error())
		return
	}
	req.ID = id
	writeJSON(w, http.StatusOK, s.store.UpsertCircular(ParseCircular(req)))
}

func (s *Server) deleteCircular(w http.ResponseWriter, r *http.Request, id string) {
	err := s.store.DeleteCircular(id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "circular_not_found", "circular not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) analyzeCircular(w http.ResponseWriter, r *http.Request, id string) {
	report, err := Analyze(s.store, id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "circular_not_found", "circular not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analysis_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) getReport(w http.ResponseWriter, r *http.Request, id string) {
	report, ok := s.store.Report(id)
	if !ok {
		writeError(w, http.StatusNotFound, "report_not_found", "run POST /circulars/{id}/analyze first")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) getClause(w http.ResponseWriter, r *http.Request, circularID, number string) {
	c, ok := s.store.Circular(circularID)
	if !ok {
		writeError(w, http.StatusNotFound, "circular_not_found", "circular not found")
		return
	}
	for _, cl := range c.Clauses {
		if cl.ClauseNumber == number {
			writeJSON(w, http.StatusOK, cl)
			return
		}
	}
	writeError(w, http.StatusNotFound, "clause_not_found", "clause not found")
}

func (s *Server) updateClause(w http.ResponseWriter, r *http.Request, circularID, number string) {
	var req ClauseUpdateRequest
	if err := decodeStrictJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "missing_text", "text is required")
		return
	}
	cl, err := s.store.UpdateClauseText(circularID, number, req.Text)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "clause_not_found", "clause not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cl)
}

func (s *Server) scanArchive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ScanArchive(s.store))
}

func (s *Server) listRelationships(w http.ResponseWriter, r *http.Request) {
	typ := r.URL.Query().Get("type")
	actionable := r.URL.Query().Get("actionable")
	items := s.store.ListRelationships()
	if typ != "" {
		filtered := items[:0]
		for _, rel := range items {
			if rel.RelationshipType == typ {
				filtered = append(filtered, rel)
			}
		}
		items = filtered
	}
	if actionable == "" || actionable == "true" || actionable == "1" {
		filtered := items[:0]
		for _, rel := range items {
			if isActionableRelationship(rel) {
				filtered = append(filtered, rel)
			}
		}
		items = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(items), "items": items})
}

func (s *Server) getRelationship(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/relationships/")
	rel, ok := s.store.Relationship(id)
	if !ok {
		writeError(w, http.StatusNotFound, "relationship_not_found", "relationship not found")
		return
	}
	writeJSON(w, http.StatusOK, rel)
}

func (s *Server) relationshipSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/relationships/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 2 && parts[1] == "deep-review" {
		s.deepReviewRelationship(w, r, parts[0])
		return
	}
	http.NotFound(w, r)
}

func (s *Server) deepReviewRelationship(w http.ResponseWriter, r *http.Request, id string) {
	rel, ok := s.store.Relationship(id)
	if !ok {
		writeError(w, http.StatusNotFound, "relationship_not_found", "relationship not found")
		return
	}
	review := deterministicDeepReview(rel)
	if cfg, ok := LLMConfigFromEnv(); ok {
		if got, err := NewLLMClient(cfg).DeepReviewRelationship(r.Context(), rel); err == nil {
			review = chooseDeepReview(rel, got, review)
		}
	}
	updated, err := s.store.SaveDeepReview(id, review)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "deep_review_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) reviewRelationship(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/relationships/")
	var req ReviewRequest
	if err := decodeStrictJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.Status != "accepted" && req.Status != "needs_followup" {
		writeError(w, http.StatusBadRequest, "invalid_status", "status must be accepted or needs_followup")
		return
	}
	rel, err := s.store.UpdateRelationshipReview(id, req)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "relationship_not_found", "relationship not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "review_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rel)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": code, "message": message})
}

func decodeStrictJSON(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 2<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain exactly one JSON document")
		}
		return err
	}
	return nil
}

func validateCircularRequest(req CircularRequest) error {
	if strings.TrimSpace(req.IssueDate) == "" {
		return nil
	}
	if _, ok := jalaliDateValue(req.IssueDate); !ok {
		return fmt.Errorf("issue_date must be a valid Jalali date in YYYY/MM/DD format")
	}
	return nil
}

func isActionableRelationship(rel Relationship) bool {
	return rel.RelationshipType != "overlap_without_conflict" && rel.RelationshipType != "neutral"
}
