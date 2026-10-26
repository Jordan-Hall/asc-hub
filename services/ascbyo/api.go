// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"context"
	"errors"
	"sync"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// Named limits.
const (
	// callTimeout bounds one call to asc. Verify and Test make a round trip to a third party, so they get
	// the longer one.
	callTimeout     = 30 * time.Second
	slowCallTimeout = 75 * time.Second
	// MaxMigrationsShown is how many migrations of a repository the page lists.
	MaxMigrationsShown = 20
)

// ErrNotConfigured is returned when [asc] is off or has no server address.
var ErrNotConfigured = errors.New("ascbyo: asc is not configured")

// API is what the package needs of asc. Tests replace it with UseAPI.
type API struct {
	Storage ascv1.StorageClient
	AI      ascv1.AiProvidersClient
}

var (
	apiMu  sync.Mutex
	apiVal *API
	// newConn is replaced by tests.
	newConn = func() (grpc.ClientConnInterface, error) {
		if !setting.Asc.Enabled || setting.Asc.ServerURL == "" {
			return nil, ErrNotConfigured
		}
		if setting.InternalToken == "" {
			return nil, errors.New("ascbyo: the internal token is not set")
		}
		return ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken})
	}
)

func getAPI() (*API, error) {
	apiMu.Lock()
	defer apiMu.Unlock()
	if apiVal != nil {
		return apiVal, nil
	}
	c, err := newConn()
	if err != nil {
		return nil, err
	}
	apiVal = &API{Storage: ascv1.NewStorageClient(c), AI: ascv1.NewAiProvidersClient(c)}
	return apiVal, nil
}

// UseAPI replaces the asc clients until restore is called. It is for tests.
func UseAPI(a API) (restore func()) {
	apiMu.Lock()
	defer apiMu.Unlock()
	old := apiVal
	apiVal = &a
	return func() {
		apiMu.Lock()
		defer apiMu.Unlock()
		apiVal = old
	}
}

// Service is the entry point. It is cheap; get one per request.
type Service struct{ api *API }

// New returns a Service, or an unavailable Failure when asc cannot be reached from this configuration.
func New() (*Service, error) {
	a, err := getAPI()
	if err != nil {
		log.Error("asc byo: %v", err)
		return nil, &Failure{Kind: FailUnavailable}
	}
	return &Service{api: a}, nil
}

// call returns the context of one call: a system call with a timeout and, when op is a UUID, its op id.
// op comes from a hidden form field; anything that does not parse is ignored and the client picks one.
func call(ctx context.Context, op string, slow bool) (context.Context, context.CancelFunc) {
	ctx = ascclient.WithSystem(ctx)
	if id, err := uuid.Parse(op); err == nil && id != uuid.Nil {
		ctx = ascclient.WithOpID(ctx, id)
	}
	d := callTimeout
	if slow {
		d = slowCallTimeout
	}
	return context.WithTimeout(ctx, d)
}

// NewOpID returns a fresh op id for a form. Submitting the same form twice then carries the same id, and
// asc answers the second submit with the first answer.
func NewOpID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return ""
	}
	return id.String()
}

// logFailure records a failed call without any payload.
func logFailure(op string, sc Scope, f *Failure) {
	switch f.Kind {
	case FailUnavailable, FailInternal, FailUnimplemented:
		log.Warn("asc byo: %s for %s failed: %s (grpc %s)", op, sc, f.Kind, f.Code)
	default:
		log.Debug("asc byo: %s for %s refused: %s (grpc %s)", op, sc, f.Kind, f.Code)
	}
}

// done classifies err, logs it and returns it as an error (nil stays nil).
func done(op string, sc Scope, err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	f := classify(err, secrets...)
	logFailure(op, sc, f)
	return f
}
