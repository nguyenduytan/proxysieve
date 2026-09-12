package memory

import (
	"context"
	"math"
	"sort"
	"sync"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

type Sources struct {
	mu   sync.RWMutex
	rows map[model.ID]store.SourceRecord
	max  int
}

func NewSources(maximum int) (*Sources, error) {
	if maximum < 1 || maximum > 1_000_000 {
		return nil, store.ErrInvalid
	}
	return &Sources{rows: map[model.ID]store.SourceRecord{}, max: maximum}, nil
}

func (s *Sources) GetSource(ctx context.Context, id model.ID) (store.SourceRecord, error) {
	if err := ctx.Err(); err != nil {
		return store.SourceRecord{}, err
	}
	if !id.Valid() {
		return store.SourceRecord{}, store.ErrInvalid
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.rows[id]
	if !ok {
		return store.SourceRecord{}, store.ErrNotFound
	}
	record.Source = record.Source.Clone()
	return record, nil
}

func (s *Sources) PutSource(ctx context.Context, source proxy.Source, expected int64) (store.SourceRecord, error) {
	if err := ctx.Err(); err != nil {
		return store.SourceRecord{}, err
	}
	if source.Validate() != nil || expected < 0 || expected == math.MaxInt64 {
		return store.SourceRecord{}, store.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.rows[source.ID]
	if expected > 0 && !exists {
		return store.SourceRecord{}, store.ErrNotFound
	}
	if exists && current.Revision != expected {
		return store.SourceRecord{}, store.ErrConflict
	}
	if !exists && len(s.rows) >= s.max {
		return store.SourceRecord{}, store.ErrUnavailable
	}
	record := store.SourceRecord{Source: source.Clone(), Revision: expected + 1}
	s.rows[source.ID] = record
	record.Source = record.Source.Clone()
	return record, nil
}

func (s *Sources) DeleteSource(ctx context.Context, id model.ID, expected int64) error {
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

func (s *Sources) ListSources(ctx context.Context, page store.Page) ([]store.SourceRecord, error) {
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
	records := make([]store.SourceRecord, 0, len(ids))
	for _, id := range ids {
		record := s.rows[model.ID(id)]
		record.Source = record.Source.Clone()
		records = append(records, record)
	}
	return records, nil
}

var _ store.Sources = (*Sources)(nil)
