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

type Pools struct {
	mu   sync.RWMutex
	rows map[model.ID]store.PoolRecord
	max  int
}

func NewPools(maximum int) (*Pools, error) {
	if maximum < 1 || maximum > 1_000_000 {
		return nil, store.ErrInvalid
	}
	return &Pools{rows: map[model.ID]store.PoolRecord{}, max: maximum}, nil
}

func (s *Pools) GetPool(ctx context.Context, id model.ID) (store.PoolRecord, error) {
	if err := ctx.Err(); err != nil {
		return store.PoolRecord{}, err
	}
	if !id.Valid() {
		return store.PoolRecord{}, store.ErrInvalid
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.rows[id]
	if !ok {
		return store.PoolRecord{}, store.ErrNotFound
	}
	record.Pool = record.Pool.Clone()
	return record, nil
}

func (s *Pools) PutPool(ctx context.Context, pool routing.Pool, expected int64) (store.PoolRecord, error) {
	if err := ctx.Err(); err != nil {
		return store.PoolRecord{}, err
	}
	if pool.Validate() != nil || expected < 0 || expected == math.MaxInt64 {
		return store.PoolRecord{}, store.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.rows[pool.ID]
	if expected > 0 && !exists {
		return store.PoolRecord{}, store.ErrNotFound
	}
	if exists && current.Revision != expected {
		return store.PoolRecord{}, store.ErrConflict
	}
	if !exists && len(s.rows) >= s.max {
		return store.PoolRecord{}, store.ErrUnavailable
	}
	record := store.PoolRecord{Pool: pool.Clone(), Revision: expected + 1}
	s.rows[pool.ID] = record
	record.Pool = record.Pool.Clone()
	return record, nil
}

func (s *Pools) DeletePool(ctx context.Context, id model.ID, expected int64) error {
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

func (s *Pools) ListPools(ctx context.Context, page store.Page) ([]store.PoolRecord, error) {
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
	records := make([]store.PoolRecord, 0, len(ids))
	for _, id := range ids {
		record := s.rows[model.ID(id)]
		record.Pool = record.Pool.Clone()
		records = append(records, record)
	}
	return records, nil
}

var _ store.Pools = (*Pools)(nil)
