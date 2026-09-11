package memory

import (
	"context"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
	"sync"
)

// Secrets is explicitly ephemeral. It is not a persistent master-key store.
type Secrets struct {
	mu     sync.RWMutex
	values map[secret.Ref]secret.Value
	max    int
}

func NewSecrets(max int) (*Secrets, error) {
	if max < 1 || max > 100_000 {
		return nil, secret.ErrInvalid
	}
	return &Secrets{values: map[secret.Ref]secret.Value{}, max: max}, nil
}
func (s *Secrets) Put(ctx context.Context, ref secret.Ref, value secret.Value) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !ref.Valid() || value.Empty() {
		return secret.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.values[ref]; !ok && len(s.values) >= s.max {
		return secret.ErrInvalid
	}
	v, err := secret.New(value.Reveal())
	if err != nil {
		return err
	}
	s.values[ref] = v
	return nil
}
func (s *Secrets) Get(ctx context.Context, ref secret.Ref) (secret.Value, error) {
	if err := ctx.Err(); err != nil {
		return secret.Value{}, err
	}
	if !ref.Valid() {
		return secret.Value{}, secret.ErrInvalid
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.values[ref]
	if !ok {
		return secret.Value{}, secret.ErrNotFound
	}
	return secret.New(v.Reveal())
}
func (s *Secrets) Delete(ctx context.Context, ref secret.Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !ref.Valid() {
		return secret.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.values[ref]; !ok {
		return secret.ErrNotFound
	}
	delete(s.values, ref)
	return nil
}

var _ secret.Store = (*Secrets)(nil)
