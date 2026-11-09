// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package migration

import "context"

// Messenger is a formatting function similar to i18n.TrString
type Messenger func(key string, args ...any)

// NilMessenger represents an empty formatting function
func NilMessenger(string, ...any) {}

type messengerKey struct{}

// WithMessenger returns ctx carrying m, for steps of a migration that run below the uploader (the git data import)
// and report how far they are.
func WithMessenger(ctx context.Context, m Messenger) context.Context {
	if m == nil {
		return ctx
	}
	return context.WithValue(ctx, messengerKey{}, m)
}

// MessengerOf returns the messenger WithMessenger put in ctx, or nil.
func MessengerOf(ctx context.Context) Messenger {
	m, _ := ctx.Value(messengerKey{}).(Messenger)
	return m
}
