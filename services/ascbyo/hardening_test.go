// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo_test

import (
	"context"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascbyo"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func setSecret(t *testing.T, v string) {
	t.Helper()
	old := setting.SecretKey
	setting.SecretKey = v
	t.Cleanup(func() { setting.SecretKey = old })
}

// stubAI answers every read with key and every change with ret, whatever was asked.
type stubAI struct {
	ascv1.AiProvidersClient
	key, ret *ascv1.AiKey
	sent     int
}

func (s *stubAI) GetKey(context.Context, *ascv1.GetAiKeyRequest, ...grpc.CallOption) (*ascv1.GetAiKeyResponse, error) {
	return &ascv1.GetAiKeyResponse{Key: s.key}, nil
}

func (s *stubAI) CreateKey(context.Context, *ascv1.CreateAiKeyRequest, ...grpc.CallOption) (*ascv1.CreateAiKeyResponse, error) {
	s.sent++
	return &ascv1.CreateAiKeyResponse{Key: s.ret}, nil
}

func (s *stubAI) UpdateKey(context.Context, *ascv1.UpdateAiKeyRequest, ...grpc.CallOption) (*ascv1.UpdateAiKeyResponse, error) {
	s.sent++
	return &ascv1.UpdateAiKeyResponse{Key: s.ret}, nil
}

type stubStorage struct {
	ascv1.StorageClient
	target, ret *ascv1.StorageTarget
	sent        int
}

func (s *stubStorage) GetTarget(context.Context, *ascv1.GetStorageTargetRequest, ...grpc.CallOption) (*ascv1.GetStorageTargetResponse, error) {
	return &ascv1.GetStorageTargetResponse{Target: s.target}, nil
}

func (s *stubStorage) CreateTarget(context.Context, *ascv1.CreateStorageTargetRequest, ...grpc.CallOption) (*ascv1.CreateStorageTargetResponse, error) {
	s.sent++
	return &ascv1.CreateStorageTargetResponse{Target: s.ret}, nil
}

func (s *stubStorage) UpdateTarget(context.Context, *ascv1.UpdateStorageTargetRequest, ...grpc.CallOption) (*ascv1.UpdateStorageTargetResponse, error) {
	s.sent++
	return &ascv1.UpdateStorageTargetResponse{Target: s.ret}, nil
}

func (s *stubStorage) SetEnabled(context.Context, *ascv1.SetStorageTargetEnabledRequest, ...grpc.CallOption) (*ascv1.SetStorageTargetEnabledResponse, error) {
	s.sent++
	return &ascv1.SetStorageTargetEnabledResponse{Target: s.ret}, nil
}

const (
	testKeyID    = "ak_abcdefghijklmnopqrst"
	testTargetID = "st_abcdefghijklmnopqrst"
)

func owner(kind ascv1.OwnerKind, name string) *ascv1.OwnerRef {
	return &ascv1.OwnerRef{Kind: kind, Name: name}
}

// TestAIKeyBaseURLNeedsNewKey proves that moving a stored key to a new address (also from none to one)
// needs the key in the same request, and that only an OpenAI-compatible key has an address at all.
func TestAIKeyBaseURLNeedsNewKey(t *testing.T) {
	s, f := newService(t)
	alice := scope(t, ascbyo.KindUser, "alice")
	compat, _, err := s.CreateKey(t.Context(), alice, &ascbyo.AIKeyInput{
		Label: "Local", Provider: "openai_compatible", APIKey: "local-key-0123456789", BaseURL: "https://llm.example.com/v1",
	}, "")
	require.NoError(t, err)
	plain, _, err := s.CreateKey(t.Context(), alice, &ascbyo.AIKeyInput{Label: "Team", Provider: "anthropic", APIKey: "sk-ant-api03-SECRETSECRET1234"}, "")
	require.NoError(t, err)
	updates := func() int { return len(f.CallsTo("UpdateKey")) }

	t.Run("a changed address without the key is a field error and sends nothing", func(t *testing.T) {
		_, changed, fe, err := s.UpdateKey(t.Context(), alice, compat.GetId(), &ascbyo.AIKeyInput{
			Label: "Local", BaseURL: "https://evil.example.net/v1",
		}, "")
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Contains(t, fe, "api_key")
		assert.Zero(t, updates())
	})
	t.Run("a cleared address without the key is refused too", func(t *testing.T) {
		_, _, fe, err := s.UpdateKey(t.Context(), alice, compat.GetId(), &ascbyo.AIKeyInput{Label: "Local"}, "")
		require.NoError(t, err)
		assert.Contains(t, fe, "api_key")
		assert.Zero(t, updates())
	})
	t.Run("an address where there was none needs the key (compatible key without one)", func(t *testing.T) {
		bare := f.AddKey(owner(ascv1.OwnerKind_OWNER_KIND_USER, "alice"), "Bare", ascv1.AiProvider_AI_PROVIDER_OPENAI_COMPATIBLE, ascv1.AiKeyState_AI_KEY_STATE_UNTESTED)
		_, _, fe, err := s.UpdateKey(t.Context(), alice, bare.GetId(), &ascbyo.AIKeyInput{Label: "Bare", BaseURL: "https://llm.example.com/v1"}, "")
		require.NoError(t, err)
		assert.Contains(t, fe, "api_key")
		assert.Zero(t, updates())

		_, changed, fe, err := s.UpdateKey(t.Context(), alice, bare.GetId(), &ascbyo.AIKeyInput{Label: "Bare", BaseURL: "https://llm.example.com/v1", APIKey: "fresh-key-0123456789"}, "")
		require.NoError(t, err)
		assert.Empty(t, fe)
		assert.True(t, changed)
	})
	t.Run("the same request with the key goes through", func(t *testing.T) {
		before := updates()
		_, changed, fe, err := s.UpdateKey(t.Context(), alice, compat.GetId(), &ascbyo.AIKeyInput{
			Label: "Local", BaseURL: "https://other.example.com/v1", APIKey: "fresh-key-9876543210",
		}, "")
		require.NoError(t, err)
		assert.Empty(t, fe)
		assert.True(t, changed)
		assert.Equal(t, before+1, updates())
	})
	t.Run("an unchanged address does not need the key", func(t *testing.T) {
		_, changed, fe, err := s.UpdateKey(t.Context(), alice, compat.GetId(), &ascbyo.AIKeyInput{
			Label: "Renamed", BaseURL: "https://other.example.com/v1",
		}, "")
		require.NoError(t, err)
		assert.Empty(t, fe)
		assert.True(t, changed)
	})
	t.Run("a provider with a fixed address refuses an address, on update and on create", func(t *testing.T) {
		before := updates()
		_, _, fe, err := s.UpdateKey(t.Context(), alice, plain.GetId(), &ascbyo.AIKeyInput{
			Label: "Team", BaseURL: "https://evil.example.net/v1", APIKey: "sk-ant-api03-NEWNEWNEWNEW",
		}, "")
		require.NoError(t, err)
		assert.Contains(t, fe, "base_url")
		assert.Equal(t, before, updates())

		for _, p := range []string{"anthropic", "openai", "google"} {
			creates := len(f.CallsTo("CreateKey"))
			_, fe, err := s.CreateKey(t.Context(), alice, &ascbyo.AIKeyInput{Label: "x", Provider: p, APIKey: "sk-0123456789abcdef", BaseURL: "https://evil.example.net/v1"}, "")
			require.NoError(t, err)
			assert.Contains(t, fe, "base_url", p)
			assert.Len(t, f.CallsTo("CreateKey"), creates, p)
		}
	})
}

// TestAIKeyMutationsCheckTheAnswerOwner proves that a changed key which asc returns is shown only when it
// is there, is the key that was asked for and belongs to the scope.
func TestAIKeyMutationsCheckTheAnswerOwner(t *testing.T) {
	alice := scope(t, ascbyo.KindUser, "alice")
	mine := owner(ascv1.OwnerKind_OWNER_KIND_USER, "Alice")
	good := &ascv1.AiKey{Id: testKeyID, Owner: mine, Label: "Team", Provider: ascv1.AiProvider_AI_PROVIDER_ANTHROPIC}
	cases := map[string]struct {
		ret  *ascv1.AiKey
		want ascbyo.FailureKind
	}{
		"nil key":           {nil, ascbyo.FailInternal},
		"another id":        {&ascv1.AiKey{Id: "ak_zzzzzzzzzzzzzzzzzzzz", Owner: mine}, ascbyo.FailNotFound},
		"another user":      {&ascv1.AiKey{Id: testKeyID, Owner: owner(ascv1.OwnerKind_OWNER_KIND_USER, "bob")}, ascbyo.FailNotFound},
		"same name, an org": {&ascv1.AiKey{Id: testKeyID, Owner: owner(ascv1.OwnerKind_OWNER_KIND_ORG, "alice")}, ascbyo.FailNotFound},
		"no owner":          {&ascv1.AiKey{Id: testKeyID}, ascbyo.FailNotFound},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			stub := &stubAI{key: good, ret: c.ret}
			t.Cleanup(ascbyo.UseAPI(ascbyo.API{AI: stub}))
			s, err := ascbyo.New()
			require.NoError(t, err)

			k, _, _, err := s.UpdateKey(t.Context(), alice, testKeyID, &ascbyo.AIKeyInput{Label: "Other"}, "")
			assert.Nil(t, k)
			assert.True(t, ascbyo.IsKind(err, c.want), "UpdateKey: %v", err)

			k, err = s.SetKeyDisabled(t.Context(), alice, testKeyID, true, "")
			assert.Nil(t, k)
			assert.True(t, ascbyo.IsKind(err, c.want), "SetKeyDisabled: %v", err)

			create := c.want
			if create == ascbyo.FailNotFound {
				create = ascbyo.FailInternal // a create names no id, so a bad answer is a server error
			}
			k, _, err = s.CreateKey(t.Context(), alice, &ascbyo.AIKeyInput{Label: "Team", Provider: "anthropic", APIKey: "sk-ant-api03-SECRETSECRET1234"}, "")
			if name == "another id" {
				// a create names no id: a new key of mine under any valid id is a good answer
				require.NoError(t, err)
				assert.NotNil(t, k)
			} else {
				assert.Nil(t, k)
				assert.True(t, ascbyo.IsKind(err, create), "CreateKey: %v", err)
			}
			assert.Equal(t, 3, stub.sent, "the calls were made; only the answers were refused")
		})
	}
	t.Run("a good answer passes", func(t *testing.T) {
		stub := &stubAI{key: good, ret: good}
		t.Cleanup(ascbyo.UseAPI(ascbyo.API{AI: stub}))
		s, err := ascbyo.New()
		require.NoError(t, err)
		k, _, _, err := s.UpdateKey(t.Context(), alice, testKeyID, &ascbyo.AIKeyInput{Label: "Other"}, "")
		require.NoError(t, err)
		assert.Equal(t, testKeyID, k.GetId())
		k, err = s.SetKeyDisabled(t.Context(), alice, testKeyID, true, "")
		require.NoError(t, err)
		assert.Equal(t, testKeyID, k.GetId())
		k, _, err = s.CreateKey(t.Context(), alice, &ascbyo.AIKeyInput{Label: "Team", Provider: "anthropic", APIKey: "sk-ant-api03-SECRETSECRET1234"}, "")
		require.NoError(t, err)
		assert.Equal(t, testKeyID, k.GetId())
	})
}

// TestStorageChangedPlaceNeedsNewCredentials proves that a changed endpoint, bucket, region or addressing
// needs the key id and the secret in the same request.
func TestStorageChangedPlaceNeedsNewCredentials(t *testing.T) {
	s, f := newService(t)
	alice := scope(t, ascbyo.KindUser, "alice")
	tg, _, err := s.CreateTarget(t.Context(), alice, goodInput(), "")
	require.NoError(t, err)
	base := func() *ascbyo.StorageInput {
		v := ascbyo.TargetViewOf(tg)
		return &ascbyo.StorageInput{Name: v.Name, Endpoint: v.Endpoint, Region: v.Region, Bucket: v.Bucket, Prefix: v.Prefix}
	}
	changes := map[string]func(*ascbyo.StorageInput){
		"endpoint":   func(in *ascbyo.StorageInput) { in.Endpoint = "https://evil.example.net" },
		"bucket":     func(in *ascbyo.StorageInput) { in.Bucket = "other-bucket" },
		"region":     func(in *ascbyo.StorageInput) { in.Region = "us-west-2" },
		"addressing": func(in *ascbyo.StorageInput) { in.Addressing = ascv1.StorageAddressing_STORAGE_ADDRESSING_PATH },
	}
	for name, change := range changes {
		t.Run(name+" without credentials", func(t *testing.T) {
			in := base()
			change(in)
			_, changed, fe, err := s.UpdateTarget(t.Context(), alice, tg.GetId(), in, "")
			require.NoError(t, err)
			assert.False(t, changed)
			assert.Contains(t, fe, "access_key_id")
			assert.Contains(t, fe, "secret_access_key")
			assert.Empty(t, f.CallsTo("UpdateTarget"))
		})
		t.Run(name+" with only the session token", func(t *testing.T) {
			in := base()
			change(in)
			in.SessionToken = "FwoGZXIvYXdzEXAMPLETOKEN"
			_, _, fe, err := s.UpdateTarget(t.Context(), alice, tg.GetId(), in, "")
			require.NoError(t, err)
			assert.NotEmpty(t, fe)
			assert.Empty(t, f.CallsTo("UpdateTarget"))
		})
	}
	t.Run("with credentials it goes through", func(t *testing.T) {
		in := base()
		in.Endpoint = "https://s3.other.example.com"
		in.AccessKeyID, in.SecretAccessKey = "AKIANEWKEYNEWKEY1234", "brand-new-secret-value"
		_, changed, fe, err := s.UpdateTarget(t.Context(), alice, tg.GetId(), in, "")
		require.NoError(t, err)
		assert.Empty(t, fe)
		assert.True(t, changed)
		req := f.CallsTo("UpdateTarget")[0].Req.(*ascv1.UpdateStorageTargetRequest)
		assert.Equal(t, "brand-new-secret-value", req.GetCredentials().GetSecretAccessKey())
	})
	t.Run("a change of name or prefix keeps the credentials", func(t *testing.T) {
		cur, err := s.GetTarget(t.Context(), alice, tg.GetId())
		require.NoError(t, err)
		v := ascbyo.TargetViewOf(cur)
		in := &ascbyo.StorageInput{Name: "renamed", Endpoint: v.Endpoint, Region: v.Region, Bucket: v.Bucket, Prefix: "other/"}
		_, changed, fe, err := s.UpdateTarget(t.Context(), alice, tg.GetId(), in, "")
		require.NoError(t, err)
		assert.Empty(t, fe)
		assert.True(t, changed)
	})
}

// TestStorageMutationsCheckTheAnswerOwner is the storage twin of the key test.
func TestStorageMutationsCheckTheAnswerOwner(t *testing.T) {
	alice := scope(t, ascbyo.KindUser, "alice")
	mine := owner(ascv1.OwnerKind_OWNER_KIND_USER, "alice")
	good := &ascv1.StorageTarget{Id: testTargetID, Owner: mine, Name: "primary", Endpoint: "https://s3.example.com", Bucket: "my-bucket"}
	cases := map[string]struct {
		ret  *ascv1.StorageTarget
		want ascbyo.FailureKind
	}{
		"nil target":   {nil, ascbyo.FailInternal},
		"another id":   {&ascv1.StorageTarget{Id: "st_zzzzzzzzzzzzzzzzzzzz", Owner: mine}, ascbyo.FailNotFound},
		"another user": {&ascv1.StorageTarget{Id: testTargetID, Owner: owner(ascv1.OwnerKind_OWNER_KIND_USER, "bob")}, ascbyo.FailNotFound},
		"an org":       {&ascv1.StorageTarget{Id: testTargetID, Owner: owner(ascv1.OwnerKind_OWNER_KIND_ORG, "alice")}, ascbyo.FailNotFound},
		"no owner":     {&ascv1.StorageTarget{Id: testTargetID}, ascbyo.FailNotFound},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			stub := &stubStorage{target: good, ret: c.ret}
			t.Cleanup(ascbyo.UseAPI(ascbyo.API{Storage: stub}))
			s, err := ascbyo.New()
			require.NoError(t, err)

			tg, _, _, err := s.UpdateTarget(t.Context(), alice, testTargetID, &ascbyo.StorageInput{
				Name: "renamed", Endpoint: "https://s3.example.com", Bucket: "my-bucket",
			}, "")
			assert.Nil(t, tg)
			assert.True(t, ascbyo.IsKind(err, c.want), "UpdateTarget: %v", err)

			tg, err = s.SetEnabled(t.Context(), alice, testTargetID, true, "")
			assert.Nil(t, tg)
			assert.True(t, ascbyo.IsKind(err, c.want), "SetEnabled: %v", err)

			create := c.want
			if create == ascbyo.FailNotFound {
				create = ascbyo.FailInternal
			}
			tg, _, err = s.CreateTarget(t.Context(), alice, goodInput(), "")
			if name == "another id" {
				require.NoError(t, err)
				assert.NotNil(t, tg)
			} else {
				assert.Nil(t, tg)
				assert.True(t, ascbyo.IsKind(err, create), "CreateTarget: %v", err)
			}
			assert.Equal(t, 3, stub.sent)
		})
	}
	t.Run("a good answer passes", func(t *testing.T) {
		stub := &stubStorage{target: good, ret: good}
		t.Cleanup(ascbyo.UseAPI(ascbyo.API{Storage: stub}))
		s, err := ascbyo.New()
		require.NoError(t, err)
		tg, _, _, err := s.UpdateTarget(t.Context(), alice, testTargetID, &ascbyo.StorageInput{Name: "renamed", Endpoint: "https://s3.example.com", Bucket: "my-bucket"}, "")
		require.NoError(t, err)
		assert.Equal(t, testTargetID, tg.GetId())
		tg, err = s.SetEnabled(t.Context(), alice, testTargetID, true, "")
		require.NoError(t, err)
		assert.Equal(t, testTargetID, tg.GetId())
		tg, _, err = s.CreateTarget(t.Context(), alice, goodInput(), "")
		require.NoError(t, err)
		assert.Equal(t, testTargetID, tg.GetId())
	})
}

// TestOpIDIsDerived proves that the op id sent to asc is an HMAC of user, scope, method and the form's id,
// keyed by the app secret.
func TestOpIDIsDerived(t *testing.T) {
	setSecret(t, "test-secret-one")
	s, f := newService(t)
	alice := scope(t, ascbyo.KindUser, "alice")
	orgScope := scope(t, ascbyo.KindOrg, "alice")
	form := ascbyo.NewOpID()

	sent := func(svc *ascbyo.Service, sc ascbyo.Scope, op string, name string) string {
		t.Helper()
		in := goodInput()
		in.Name = name
		_, _, err := svc.CreateTarget(t.Context(), sc, in, op)
		require.NoError(t, err)
		c := f.CallsTo("CreateTarget")
		got := c[len(c)-1].OpID
		id, perr := uuid.Parse(got)
		require.NoError(t, perr, "the op id is a uuid")
		assert.NotEqual(t, uuid.Nil, id)
		return got
	}

	base := sent(s.ForUser(7), alice, form, "one")
	assert.NotEqual(t, form, base, "never the client's id")
	assert.Equal(t, base, sent(s.ForUser(7), alice, form, "two"), "the same inputs give the same id, so a double submit is one action")
	assert.NotEqual(t, base, sent(s.ForUser(8), alice, form, "three"), "another user")
	assert.NotEqual(t, base, sent(s.ForUser(7), orgScope, form, "four"), "another scope")
	assert.NotEqual(t, base, sent(s.ForUser(7), alice, ascbyo.NewOpID(), "five"), "another form id")

	// the method is part of it: the same form id on another call is another op
	tg := f.AddTarget(owner(ascv1.OwnerKind_OWNER_KIND_USER, "alice"), "planted", ascv1.StorageTargetState_STORAGE_TARGET_STATE_VERIFIED)
	_, err := s.ForUser(7).SetEnabled(t.Context(), alice, tg.GetId(), true, form)
	require.NoError(t, err)
	enable := f.CallsTo("SetEnabled")[0].OpID
	assert.NotEqual(t, base, enable, "another method")
	assert.NotEqual(t, form, enable)

	// a key that depends on the app secret
	setSecret(t, "test-secret-two")
	assert.NotEqual(t, base, sent(s.ForUser(7), alice, form, "six"), "another app secret")

	t.Run("a form id that is not a uuid is not used", func(t *testing.T) {
		got := sent(s.ForUser(7), alice, "not-a-uuid\r\nx-evil: 1", "seven")
		assert.NotContains(t, got, "evil")
	})
	t.Run("the other mutating calls derive too", func(t *testing.T) {
		setSecret(t, "test-secret-one")
		k, _, err := s.ForUser(7).CreateKey(t.Context(), alice, &ascbyo.AIKeyInput{Label: "Team", Provider: "anthropic", APIKey: "sk-ant-api03-SECRETSECRET1234"}, form)
		require.NoError(t, err)
		c := f.CallsTo("CreateKey")
		assert.NotEqual(t, form, c[len(c)-1].OpID)
		_, err = s.ForUser(7).SetKeyDisabled(t.Context(), alice, k.GetId(), true, form)
		require.NoError(t, err)
		_, err = s.ForUser(7).TestKey(t.Context(), alice, k.GetId(), form)
		require.NoError(t, err)
		require.NoError(t, s.ForUser(7).DeleteKey(t.Context(), alice, k.GetId(), form))
		seen := map[string]bool{}
		for _, m := range []string{"CreateKey", "UpdateKey", "TestKey", "DeleteKey"} {
			for _, call := range f.CallsTo(m) {
				assert.NotEqual(t, form, call.OpID, m)
				seen[call.OpID] = true
			}
		}
		assert.Len(t, seen, 4, "one id per method")
	})
}
