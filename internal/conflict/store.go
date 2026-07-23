package conflict

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")

// Store مخزن همزمان‌امن سرویس است.
//
// داده‌ها فعلاً در حافظه نگه داشته می‌شوند تا سرویس بدون وابستگی بالا بیاید.
// برای نسخه عملیاتی، همین مرز باید با Postgres جایگزین شود.
type Store struct {
	mu            sync.RWMutex
	circulars     map[string]Circular
	relationships map[string]Relationship
	reports       map[string]Report
	nextID        int
	statePath     string
	db            *sql.DB
}

// NewStore یک مخزن خالی می‌سازد.
func NewStore(statePath ...string) *Store {
	s := &Store{
		circulars:     map[string]Circular{},
		relationships: map[string]Relationship{},
		reports:       map[string]Report{},
		nextID:        1,
	}
	if len(statePath) > 0 && statePath[0] != "" {
		s.statePath = statePath[0]
		_ = s.Load()
	}
	return s
}

// NewPostgresStore مخزن را با پایداری PostgreSQL می‌سازد.
//
// برای ساده نگه داشتن منطق دامنه، وضعیت سرویس به صورت یک سند JSONB ذخیره
// می‌شود. اگر حجم داده زیاد شد، همین مرز به جدول‌های جداگانه تبدیل می‌شود.
func NewPostgresStore(ctx context.Context, db *sql.DB) (*Store, error) {
	s := NewStore()
	s.db = db
	if err := s.initPostgres(ctx); err != nil {
		return nil, err
	}
	if err := s.Load(); err != nil {
		return nil, err
	}
	return s, nil
}

// UpsertCircular بخشنامه را با شناسه خودش ثبت یا جایگزین می‌کند.
func (s *Store) UpsertCircular(c Circular) Circular {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == "" {
		c.ID = s.nextCircularID()
	}
	if existing, exists := s.circulars[c.ID]; exists && !sameCircularContent(existing, c) {
		s.removeCircularAnalysisLocked(c.ID)
	}
	s.circulars[c.ID] = c
	_ = s.saveLocked()
	return c
}

// HasCircular وجود یک بخشنامه را بررسی می‌کند.
func (s *Store) HasCircular(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.circulars[id]
	return ok
}

// DeleteCircular یک بخشنامه و گزارش‌های مستقیم آن را حذف می‌کند.
func (s *Store) DeleteCircular(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.circulars[id]; !ok {
		return ErrNotFound
	}
	delete(s.circulars, id)
	s.removeCircularAnalysisLocked(id)
	_ = s.saveLocked()
	return nil
}

// removeCircularAnalysisLocked گزارش‌ها و روابط وابسته به بخشنامه را حذف می‌کند.
func (s *Store) removeCircularAnalysisLocked(id string) {
	delete(s.reports, id)
	for rid, rel := range s.relationships {
		if strings.HasPrefix(rel.SourceClauseID, id+"#") || strings.HasPrefix(rel.TargetClauseID, id+"#") {
			delete(s.relationships, rid)
		}
	}
	for reportID, report := range s.reports {
		kept := report.Relationships[:0]
		for _, rel := range report.Relationships {
			if !strings.HasPrefix(rel.SourceClauseID, id+"#") && !strings.HasPrefix(rel.TargetClauseID, id+"#") {
				kept = append(kept, rel)
			}
		}
		report.Relationships = kept
		report.Summary = summarize(kept)
		report.PlainLanguageSummary = deterministicComplianceSummary(importantRelationships(kept, 8))
		report.SummaryGeneratedByLLM = false
		s.reports[reportID] = report
	}
}

// Circular یک بخشنامه را با شناسه برمی‌گرداند.
func (s *Store) Circular(id string) (Circular, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.circulars[id]
	return c, ok
}

// ListCirculars فهرست مرتب بخشنامه‌ها را برمی‌گرداند.
func (s *Store) ListCirculars() []Circular {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Circular, 0, len(s.circulars))
	for _, c := range s.circulars {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// CircularCount تعداد بخشنامه‌های ثبت‌شده را برمی‌گرداند.
func (s *Store) CircularCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.circulars)
}

// Persistent نشان می‌دهد ذخیره‌سازی دیسکی فعال است یا نه.
func (s *Store) Persistent() bool {
	return s.statePath != "" || s.db != nil
}

// ImportFrom وضعیت یک مخزن دیگر را در این مخزن جایگزین و ذخیره می‌کند.
func (s *Store) ImportFrom(source *Store) error {
	state, err := source.snapshot()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyStateLocked(state)
	return s.saveLocked()
}

// Clause بند و بخشنامه مادر را با شناسه بند پیدا می‌کند.
func (s *Store) Clause(id string) (Clause, Circular, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.circulars {
		for _, cl := range c.Clauses {
			if cl.ID == id {
				return cl, c, true
			}
		}
	}
	return Clause{}, Circular{}, false
}

// SaveReport گزارش و روابط داخل آن را ذخیره می‌کند.
func (s *Store) SaveReport(report Report) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range report.Relationships {
		if existing, ok := s.relationships[report.Relationships[i].ID]; ok {
			report.Relationships[i] = preserveRelationshipReview(report.Relationships[i], existing)
		}
	}
	s.reports[report.CircularID] = report
	for _, r := range report.Relationships {
		s.relationships[r.ID] = r
	}
	_ = s.saveLocked()
}

func preserveRelationshipReview(next, existing Relationship) Relationship {
	next.ReviewStatus = existing.ReviewStatus
	next.ReviewedBy = existing.ReviewedBy
	next.ReviewNote = existing.ReviewNote
	next.ReviewedAt = existing.ReviewedAt
	if existing.DeepReview != nil {
		review := chooseDeepReview(next, *existing.DeepReview, deterministicDeepReview(next))
		next.DeepReview = &review
	}
	return next
}

// Report گزارش ذخیره‌شده برای یک بخشنامه یا archive را برمی‌گرداند.
func (s *Store) Report(circularID string) (Report, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.reports[circularID]
	return r, ok
}

// ListRelationships همه روابط ذخیره‌شده را مرتب برمی‌گرداند.
func (s *Store) ListRelationships() []Relationship {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Relationship, 0, len(s.relationships))
	for _, r := range s.relationships {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Relationship یک رابطه ذخیره‌شده را برمی‌گرداند.
func (s *Store) Relationship(id string) (Relationship, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.relationships[id]
	return r, ok
}

// SaveDeepReview نتیجه بررسی عمیق را روی رابطه و گزارش‌های مربوط ذخیره می‌کند.
func (s *Store) SaveDeepReview(id string, review DeepReview) (Relationship, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rel, ok := s.relationships[id]
	if !ok {
		return Relationship{}, ErrNotFound
	}
	rel.DeepReview = &review
	s.relationships[id] = rel
	for rid, report := range s.reports {
		for i := range report.Relationships {
			if report.Relationships[i].ID == id {
				report.Relationships[i] = rel
			}
		}
		s.reports[rid] = report
	}
	_ = s.saveLocked()
	return rel, nil
}

// UpdateRelationshipReview تصمیم انسانی را روی یک رابطه ذخیره می‌کند.
func (s *Store) UpdateRelationshipReview(id string, req ReviewRequest) (Relationship, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rel, ok := s.relationships[id]
	if !ok {
		return Relationship{}, ErrNotFound
	}
	now := nowUTC()
	rel.ReviewStatus = req.Status
	rel.ReviewedBy = req.By
	rel.ReviewNote = req.Note
	rel.ReviewedAt = &now
	s.relationships[id] = rel
	for rid, report := range s.reports {
		for i := range report.Relationships {
			if report.Relationships[i].ID == id {
				report.Relationships[i] = rel
			}
		}
		s.reports[rid] = report
	}
	_ = s.saveLocked()
	return rel, nil
}

// UpdateClauseText متن یک بند را اصلاح و بند را دوباره استخراج می‌کند.
func (s *Store) UpdateClauseText(circularID, number, text string) (Clause, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.circulars[circularID]
	if !ok {
		return Clause{}, ErrNotFound
	}
	for i := range c.Clauses {
		if c.Clauses[i].ClauseNumber == number {
			s.removeCircularAnalysisLocked(circularID)
			updated := buildClause(c, number, text)
			c.Clauses[i] = updated
			c.RawText = rebuildRawText(c.Clauses)
			c.NormalizedText = NormalizePersian(c.RawText)
			s.circulars[circularID] = c
			delete(s.reports, circularID)
			_ = s.saveLocked()
			return updated, nil
		}
	}
	return Clause{}, ErrNotFound
}

type storeState struct {
	Circulars     map[string]Circular     `json:"circulars"`
	Relationships map[string]Relationship `json:"relationships"`
	Reports       map[string]Report       `json:"reports"`
	NextID        int                     `json:"next_id"`
}

// Load وضعیت ذخیره‌شده را از دیسک می‌خواند.
func (s *Store) Load() error {
	if s.db != nil {
		return s.loadPostgres(context.Background())
	}
	if s.statePath == "" {
		return nil
	}
	b, err := os.ReadFile(s.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var state storeState
	if err := json.Unmarshal(b, &state); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyStateLocked(state)
	return nil
}

func (s *Store) saveLocked() error {
	if s.db != nil {
		return s.savePostgresLocked(context.Background())
	}
	if s.statePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.statePath), 0o700); err != nil {
		return err
	}
	state := storeState{
		Circulars:     s.circulars,
		Relationships: s.relationships,
		Reports:       s.reports,
		NextID:        s.nextID,
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.statePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.statePath)
}

func (s *Store) initPostgres(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS conflict_state (
	key text PRIMARY KEY,
	value jsonb NOT NULL,
	updated_at timestamptz NOT NULL DEFAULT now()
)`)
	return err
}

func (s *Store) loadPostgres(ctx context.Context) error {
	var b []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM conflict_state WHERE key = 'default'`).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var state storeState
	if err := json.Unmarshal(b, &state); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyStateLocked(state)
	return nil
}

func (s *Store) applyStateLocked(state storeState) {
	if state.Circulars != nil {
		s.circulars = state.Circulars
	}
	if state.Relationships != nil {
		s.relationships = state.Relationships
	}
	if state.Reports != nil {
		s.reports = state.Reports
	}
	if state.NextID > 0 {
		s.nextID = state.NextID
	}
}

func (s *Store) snapshot() (storeState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, err := json.Marshal(storeState{
		Circulars:     s.circulars,
		Relationships: s.relationships,
		Reports:       s.reports,
		NextID:        s.nextID,
	})
	if err != nil {
		return storeState{}, err
	}
	var state storeState
	if err := json.Unmarshal(b, &state); err != nil {
		return storeState{}, err
	}
	return state, nil
}

func (s *Store) savePostgresLocked(ctx context.Context) error {
	state := storeState{
		Circulars:     s.circulars,
		Relationships: s.relationships,
		Reports:       s.reports,
		NextID:        s.nextID,
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO conflict_state (key, value, updated_at)
VALUES ('default', $1, now())
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, b)
	return err
}

func sameCircularContent(a, b Circular) bool {
	return a.ID == b.ID &&
		a.Title == b.Title &&
		a.RawText == b.RawText &&
		a.IssuerUnit == b.IssuerUnit &&
		a.CircularType == b.CircularType &&
		a.Topic == b.Topic &&
		a.IssueDate == b.IssueDate &&
		a.EffectiveDate == b.EffectiveDate &&
		a.Status == b.Status
}

func (s *Store) nextCircularID() string {
	for {
		id := "C-" + leftPadInt(s.nextID, 4)
		s.nextID++
		if _, exists := s.circulars[id]; !exists {
			return id
		}
	}
}

func rebuildRawText(clauses []Clause) string {
	out := make([]string, 0, len(clauses))
	for _, cl := range clauses {
		out = append(out, cl.OriginalText)
	}
	return strings.Join(out, "\n")
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

func leftPadInt(n, width int) string {
	digits := "0123456789"
	if n == 0 {
		return "0000"[:width-1] + "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{digits[n%10]}, b...)
		n /= 10
	}
	for len(b) < width {
		b = append([]byte{'0'}, b...)
	}
	return string(b)
}
