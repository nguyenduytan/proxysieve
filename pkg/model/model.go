// Copyright 2026 Tony Nguyen
// SPDX-License-Identifier: Apache-2.0

// Package model contains small shared identifiers and validation errors.
package model

import (
	"crypto/rand"
	"errors"
	"regexp"
)

var ErrInvalid = errors.New("invalid domain value")

// ID is an opaque stable identifier, not a user-visible name or credential.
type ID string

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func (id ID) Valid() bool { return identifier.MatchString(string(id)) }

// NewID generates a cryptographically random identifier without embedding user data.
func NewID() ID { return ID(rand.Text()) }

// Optional preserves unknown versus known empty/zero values at protocol boundaries.
type Optional[T any] struct {
	Known bool `json:"known"`
	Value T    `json:"value"`
}
