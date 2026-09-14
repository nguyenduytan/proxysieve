package memory

import (
	"context"
	"math"
	"sort"
	"sync"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type Policies struct {
	mu   sync.RWMutex
	rows map[model.ID]store.PolicyRecord
	max  int
}

func NewPolicies(maximum int) (*Policies, error) {
	if maximum < 1 || maximum > 1_000_000 {
		return nil, store.ErrInvalid
	}
	return &Policies{rows: map[model.ID]store.PolicyRecord{}, max: maximum}, nil
}

func (s *Policies) GetPolicy(ctx context.Context, id model.ID) (store.PolicyRecord, error) {
	if err := ctx.Err(); err != nil {
		return store.PolicyRecord{}, err
	}
	if !id.Valid() {
		return store.PolicyRecord{}, store.ErrInvalid
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.rows[id]
	if !ok {
		return store.PolicyRecord{}, store.ErrNotFound
	}
	record.Policy = record.Policy.Clone()
	return record, nil
}

func (s *Policies) PutPolicy(ctx context.Context, document policy.Policy, expected int64) (store.PolicyRecord, error) {
	if err := ctx.Err(); err != nil {
		return store.PolicyRecord{}, err
	}
	if document.Validate() != nil || expected < 0 || expected == math.MaxInt64 {
		return store.PolicyRecord{}, store.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.rows[document.ID]
	if expected > 0 && !exists {
		return store.PolicyRecord{}, store.ErrNotFound
	}
	if exists && current.Revision != expected {
		return store.PolicyRecord{}, store.ErrConflict
	}
	if !exists && len(s.rows) >= s.max {
		return store.PolicyRecord{}, store.ErrUnavailable
	}
	record := store.PolicyRecord{Policy: document.Clone(), Revision: expected + 1}
	s.rows[document.ID] = record
	record.Policy = record.Policy.Clone()
	return record, nil
}

func (s *Policies) DeletePolicy(ctx context.Context, id model.ID, expected int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !id.Valid() || expected < 1 {
		return store.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.rows[id]
	if !ok {
		return store.ErrNotFound
	}
	if record.Revision != expected {
		return store.ErrConflict
	}
	delete(s.rows, id)
	return nil
}

func (s *Policies) ListPolicies(ctx context.Context, page store.Page) ([]store.PolicyRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := page.Validate(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.rows))
	for id := range s.rows {
		if id > page.After {
			ids = append(ids, string(id))
		}
	}
	sort.Strings(ids)
	if len(ids) > page.Limit {
		ids = ids[:page.Limit]
	}
	records := make([]store.PolicyRecord, 0, len(ids))
	for _, id := range ids {
		record := s.rows[model.ID(id)]
		record.Policy = record.Policy.Clone()
		records = append(records, record)
	}
	return records, nil
}

var _ store.Policies = (*Policies)(nil)
