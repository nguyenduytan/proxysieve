package memory

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
	"testing"
)

func TestSecretStore(t *testing.T) {
	s, err := NewSecrets(1)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := secret.New([]byte("fake-password"))
	ctx := t.Context()
	if err = s.Put(ctx, "secret://one", v); err != nil {
		t.Fatal(err)
	}
	if err = s.Put(ctx, "secret://two", v); err == nil {
		t.Fatal("unbounded store")
	}
	got, err := s.Get(ctx, "secret://one")
	if err != nil || string(got.Reveal()) != "fake-password" {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, "secret://one"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, "secret://one"); !errors.Is(err, secret.ErrNotFound) {
		t.Fatal(err)
	}
	c, cancel := context.WithCancel(ctx)
	cancel()
	if err = s.Put(c, "secret://one", v); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
