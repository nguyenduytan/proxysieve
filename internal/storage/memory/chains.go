package memory

import (
	"context"
	"math"
	"sort"
	"sync"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type Chains struct {
	mu   sync.RWMutex
	rows map[model.ID]store.ChainRecord
	max  int
}

func NewChains(maximum int) (*Chains, error) {
	if maximum < 1 || maximum > 1_000_000 {
		return nil, store.ErrInvalid
	}
	return &Chains{rows: map[model.ID]store.ChainRecord{}, max: maximum}, nil
}

func (s *Chains) GetChain(ctx context.Context, id model.ID) (store.ChainRecord, error) {
	if err := ctx.Err(); err != nil {
		return store.ChainRecord{}, err
	}
	if !id.Valid() {
		return store.ChainRecord{}, store.ErrInvalid
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.rows[id]
	if !ok {
		return store.ChainRecord{}, store.ErrNotFound
	}
	record.Chain = record.Chain.Clone()
	return record, nil
}

func (s *Chains) PutChain(ctx context.Context, chain routing.Chain, expected int64) (store.ChainRecord, error) {
	if err := ctx.Err(); err != nil {
		return store.ChainRecord{}, err
	}
	if chain.Validate() != nil || expected < 0 || expected == math.MaxInt64 {
		return store.ChainRecord{}, store.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.rows[chain.ID]
	if expected > 0 && !exists {
		return store.ChainRecord{}, store.ErrNotFound
	}
	if exists && current.Revision != expected {
		return store.ChainRecord{}, store.ErrConflict
	}
	if !exists && len(s.rows) >= s.max {
		return store.ChainRecord{}, store.ErrUnavailable
	}
	record := store.ChainRecord{Chain: chain.Clone(), Revision: expected + 1}
	s.rows[chain.ID] = record
	record.Chain = record.Chain.Clone()
	return record, nil
}

func (s *Chains) DeleteChain(ctx context.Context, id model.ID, expected int64) error {
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

func (s *Chains) ListChains(ctx context.Context, page store.Page) ([]store.ChainRecord, error) {
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
	records := make([]store.ChainRecord, 0, len(ids))
	for _, id := range ids {
		record := s.rows[model.ID(id)]
		record.Chain = record.Chain.Clone()
		records = append(records, record)
	}
	return records, nil
}

var _ store.Chains = (*Chains)(nil)
