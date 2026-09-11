// Copyright 2026 Tony Nguyen
// SPDX-License-Identifier: Apache-2.0

// Package secret defines explicit secret boundaries and safe default serialization.
package secret

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
)

const Redacted = "[REDACTED]"
const MaxBytes = 64 * 1024

var (
	ErrInvalid  = errors.New("invalid secret material or reference")
	ErrNotFound = errors.New("secret not found")
)

type Ref string

var refPattern = regexp.MustCompile(`^secret://[A-Za-z0-9_-]+(?:/[A-Za-z0-9_-]+){0,7}$`)

func (r Ref) Valid() bool { return len(r) <= 256 && refPattern.MatchString(string(r)) }

// Value intentionally has no exported secret-bearing field. Reveal is the explicit
// boundary for transport/encryption code; normal formatting and JSON stay redacted.
type Value struct{ raw []byte }

func New(raw []byte) (Value, error) {
	if len(raw) == 0 || len(raw) > MaxBytes {
		return Value{}, ErrInvalid
	}
	return Value{raw: bytes.Clone(raw)}, nil
}

func (v Value) Reveal() []byte             { return bytes.Clone(v.raw) }
func (v Value) Empty() bool                { return len(v.raw) == 0 }
func (Value) String() string               { return Redacted }
func (Value) GoString() string             { return Redacted }
func (Value) Format(s fmt.State, _ rune)   { _, _ = io.WriteString(s, Redacted) }
func (Value) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }
func (Value) MarshalText() ([]byte, error) { return []byte(Redacted), nil }
func (Value) LogValue() slog.Value         { return slog.StringValue(Redacted) }

// Store is the explicit credential boundary. Raw material must never be persisted
// through an ordinary metadata repository. Implementations must copy values.
type Store interface {
	Put(context.Context, Ref, Value) error
	Get(context.Context, Ref) (Value, error)
	Delete(context.Context, Ref) error
}
