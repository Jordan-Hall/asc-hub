// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strconv"
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
type Service struct {
	api *API
	// user is the id of the signed-in user the calls are made for. It is part of every derived op id, so one
	// user cannot replay or pre-empt the op id of another.
	user int64
}

// ForUser returns a Service whose op ids are bound to the signed-in user with this id.
func (s *Service) ForUser(userID int64) *Service {
	c := *s
	c.user = userID
	return &c
}

// New returns a Service, or an unavailable Failure when asc cannot be reached from this configuration.
func New() (*Service, error) {
	a, err := getAPI()
	if err != nil {
		log.Error("asc byo: %v", err)
		return nil, &Failure{Kind: FailUnavailable}
	}
	return &Service{api: a}, nil
}

// opIDDomain separates the op id key from every other use of the app secret.
const opIDDomain = "asc byo op id v1"

// deriveOpID turns the op id a form carried into the one sent to asc: an HMAC, keyed by the app secret, of
// the user, the scope, the method and the form's id. asc replays the first answer of an op id, so an id
// taken as sent would let a client choose which earlier answer (of another user, scope or method) it gets
// back. It returns "" when the form id is not a UUID or the app secret is not set; the client then picks a
// fresh id.
func deriveOpID(secret string, user int64, sc Scope, method, formOp string) string {
	id, err := uuid.Parse(formOp)
	if err != nil || id == uuid.Nil || secret == "" {
		return ""
	}
	key := sha256.Sum256([]byte(opIDDomain + "\x00" + secret))
	m := hmac.New(sha256.New, key[:])
	for _, part := range [][]byte{[]byte(strconv.FormatInt(user, 10)), []byte(sc.String()), []byte(method), id[:]} {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(part)))
		m.Write(n[:])
		m.Write(part)
	}
	var out uuid.UUID
	copy(out[:], m.Sum(nil))
	out[6] = out[6]&0x0f | 0x80 // version 8: custom
	out[8] = out[8]&0x3f | 0x80 // RFC 4122 variant
	return out.String()
}

// call returns the context of one call: a system call with a timeout and, when op is a UUID, an op id
// derived from it (see deriveOpID). op comes from a hidden form field and is never sent as it is; anything
// that does not parse is ignored and the client picks one. method names the call, sc the scope it is for.
func (s *Service) call(ctx context.Context, sc Scope, method, op string, slow bool) (context.Context, context.CancelFunc) {
	ctx = ascclient.WithSystem(ctx)
	if d := deriveOpID(setting.SecretKey, s.user, sc, method, op); d != "" {
		if id, err := uuid.Parse(d); err == nil {
			ctx = ascclient.WithOpID(ctx, id)
		}
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
