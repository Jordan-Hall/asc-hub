// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascclient

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/blake3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// stock builds a file object and puts its stored form on the fake server.
func stock(t *testing.T, s *fakeServer, c *Client, kind ascv1.ObjKind, data []byte) *ascv1.Object {
	t.Helper()
	o, err := c.NewObject(kind, data)
	require.NoError(t, err)
	s.repo.mu.Lock()
	s.repo.store[objectKeyName(o.Key)] = o.Data
	s.repo.mu.Unlock()
	return o
}

func TestObjectIDMatchesTheSpecContexts(t *testing.T) {
	// SPEC section 2.1: id = blake3 derive-key(context, bytes).
	data := []byte("hello")
	for kind, ctx := range idContext {
		h := blake3.NewDeriveKey(ctx)
		_, _ = h.Write(data)
		got, err := ObjectID(kind, data)
		require.NoError(t, err)
		assert.Equal(t, h.Sum(nil), got, ctx)
	}
	a, _ := ObjectID(ascv1.ObjKind_OBJ_KIND_FILE, data)
	b, _ := ObjectID(ascv1.ObjKind_OBJ_KIND_CHUNK, data)
	assert.NotEqual(t, a, b, "kinds are domain separated")
	_, err := ObjectID(ascv1.ObjKind_OBJ_KIND_CHANGE_META, data)
	require.ErrorIs(t, err, ErrUnsupportedKind)
}

func TestStoredCodecRoundTripAndLimits(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, func(cfg *Config) { cfg.MaxDecodedBytes = 1 << 20 })

	compressible := bytes.Repeat([]byte("abcdefgh"), 4096)
	z := c.encodeStored(compressible)
	assert.Equal(t, byte(codecZstd), z[0])
	assert.Less(t, len(z), len(compressible)/2)
	got, err := c.decodeStored(z)
	require.NoError(t, err)
	assert.Equal(t, compressible, got)

	tiny := c.encodeStored([]byte("x"))
	assert.Equal(t, byte(codecRaw), tiny[0], "tiny values stay raw")
	got, err = c.decodeStored(tiny)
	require.NoError(t, err)
	assert.Equal(t, []byte("x"), got)

	empty, err := c.decodeStored([]byte{codecRaw})
	require.NoError(t, err)
	assert.Empty(t, empty)

	for name, bad := range map[string][]byte{
		"empty":         {},
		"unknown codec": {7, 1, 2, 3},
		"junk zstd":     append([]byte{codecZstd}, 1, 2, 3, 4, 5),
		"raw too big":   append([]byte{codecRaw}, make([]byte, 1<<20+1)...),
	} {
		_, err := c.decodeStored(bad)
		assert.ErrorIs(t, err, ErrCorrupt, name)
	}

	// A frame that declares more than the cap is refused before allocation (a zstd bomb).
	big := bytes.Repeat([]byte{0}, 4<<20)
	enc, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	frame := enc.EncodeAll(big, nil)
	_, err = c.decodeStored(append([]byte{codecZstd}, frame...))
	assert.ErrorIs(t, err, ErrCorrupt)

	// A frame without a declared content size is refused.
	var sink bytes.Buffer
	sw, err := zstd.NewWriter(&sink)
	require.NoError(t, err)
	_, _ = sw.Write([]byte("streamed, so no content size"))
	require.NoError(t, sw.Close())
	_, err = c.decodeStored(append([]byte{codecZstd}, sink.Bytes()...))
	assert.ErrorIs(t, err, ErrCorrupt)

	// A skippable frame is refused.
	skip := binary.LittleEndian.AppendUint32(nil, 0x184D2A50)
	skip = binary.LittleEndian.AppendUint32(skip, 0)
	_, err = c.decodeStored(append([]byte{codecZstd}, skip...))
	assert.ErrorIs(t, err, ErrCorrupt)
}

func TestGetObjectsVerifiesCachesAndReportsMissing(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, nil)
	ctx := userCtx(t, "alice")

	file := stock(t, s, c, ascv1.ObjKind_OBJ_KIND_FILE, bytes.Repeat([]byte("package main\n"), 500))
	empty := stock(t, s, c, ascv1.ObjKind_OBJ_KIND_FILE, nil)
	tree := stock(t, s, c, ascv1.ObjKind_OBJ_KIND_TREE, []byte("tree envelope"))
	absent, err := c.NewObject(ascv1.ObjKind_OBJ_KIND_FILE, []byte("not stored"))
	require.NoError(t, err)

	keys := []*ascv1.ObjKey{file.Key, absent.Key, tree.Key, file.Key, empty.Key}
	got, err := c.GetObjects(ctx, "o/r", keys)
	require.NoError(t, err)
	require.Len(t, got, 5)
	assert.Equal(t, bytes.Repeat([]byte("package main\n"), 500), got[0])
	assert.Nil(t, got[1], "missing")
	assert.Equal(t, []byte("tree envelope"), got[2])
	assert.Equal(t, got[0], got[3], "a duplicate key is answered once")
	assert.NotNil(t, got[4])
	assert.Empty(t, got[4])
	require.Len(t, s.repo.getKeys, 1)
	assert.Len(t, s.repo.getKeys[0], 4, "duplicates are not sent twice")

	// A second read of the same objects by the same principal is served from the cache.
	again, err := c.GetObjects(ctx, "o/r", []*ascv1.ObjKey{tree.Key, file.Key})
	require.NoError(t, err)
	assert.Equal(t, [][]byte{got[2], got[0]}, again)
	assert.Equal(t, 1, s.repo.getCalls)
	st := c.CacheStats()
	assert.Equal(t, uint64(2), st.Hits)
	assert.Equal(t, 3, st.Entries, "file, empty and tree are cached; the missing key is not")
}

func TestCacheNeverCrossesPrincipalOrRepository(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, nil)
	o := stock(t, s, c, ascv1.ObjKind_OBJ_KIND_FILE, []byte("secret of repo a"))

	_, err := c.GetObjects(userCtx(t, "alice"), "o/a", []*ascv1.ObjKey{o.Key})
	require.NoError(t, err)
	require.Equal(t, 1, s.repo.getCalls)

	// bob must be authorised by the server himself, even though alice already fetched the object.
	_, err = c.GetObjects(userCtx(t, "bob"), "o/a", []*ascv1.ObjKey{o.Key})
	require.NoError(t, err)
	assert.Equal(t, 2, s.repo.getCalls)
	// so must alice for another repository, and the system principal.
	_, err = c.GetObjects(userCtx(t, "alice"), "o/b", []*ascv1.ObjKey{o.Key})
	require.NoError(t, err)
	assert.Equal(t, 3, s.repo.getCalls)
	_, err = c.GetObjects(WithSystem(t.Context()), "o/a", []*ascv1.ObjKey{o.Key})
	require.NoError(t, err)
	assert.Equal(t, 4, s.repo.getCalls)
	// and the same principal and repository hit.
	_, err = c.GetObjects(userCtx(t, "alice"), "o/a", []*ascv1.ObjKey{o.Key})
	require.NoError(t, err)
	assert.Equal(t, 4, s.repo.getCalls)

	// Without any credential nothing is served, not even from the cache.
	_, err = c.GetObjects(t.Context(), "o/a", []*ascv1.ObjKey{o.Key})
	require.ErrorIs(t, err, ErrNoCredential)
}

func TestCacheCanBeDisabled(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, func(cfg *Config) { cfg.CacheBytes = -1 })
	o := stock(t, s, c, ascv1.ObjKind_OBJ_KIND_FILE, []byte("x"))
	for range 2 {
		_, err := c.GetObjects(WithSystem(t.Context()), "o/r", []*ascv1.ObjKey{o.Key})
		require.NoError(t, err)
	}
	assert.Equal(t, 2, s.repo.getCalls)
	assert.Equal(t, CacheStats{}, c.CacheStats())
}

func TestGetObjectsRejectsWhatTheServerMustNotSend(t *testing.T) {
	good := []byte("good content")
	for name, tc := range map[string]struct {
		hook func(c *Client, req *ascv1.GetObjectsRequest) *ascv1.GetObjectsResponse
	}{
		"wrong content under the right id": {func(c *Client, req *ascv1.GetObjectsRequest) *ascv1.GetObjectsResponse {
			return &ascv1.GetObjectsResponse{Objects: []*ascv1.Object{{Key: req.Keys[0], Data: append([]byte{codecRaw}, "evil content"...)}}}
		}},
		"content of another kind": {func(c *Client, req *ascv1.GetObjectsRequest) *ascv1.GetObjectsResponse {
			// the id was computed as a file; the same bytes as a chunk hash differently.
			return &ascv1.GetObjectsResponse{Objects: []*ascv1.Object{{
				Key: &ascv1.ObjKey{Kind: ascv1.ObjKind_OBJ_KIND_CHUNK, Id: req.Keys[0].Id}, Data: append([]byte{codecRaw}, good...),
			}}}
		}},
		"an object that was not requested": {func(c *Client, req *ascv1.GetObjectsRequest) *ascv1.GetObjectsResponse {
			o, _ := c.NewObject(ascv1.ObjKind_OBJ_KIND_FILE, []byte("other"))
			return &ascv1.GetObjectsResponse{Objects: []*ascv1.Object{o}}
		}},
		"an unrequested key reported missing": {func(c *Client, req *ascv1.GetObjectsRequest) *ascv1.GetObjectsResponse {
			return &ascv1.GetObjectsResponse{Missing: []*ascv1.ObjKey{{Kind: ascv1.ObjKind_OBJ_KIND_FILE, Id: bytes.Repeat([]byte{9}, 32)}}}
		}},
		"a short id": {func(c *Client, req *ascv1.GetObjectsRequest) *ascv1.GetObjectsResponse {
			return &ascv1.GetObjectsResponse{Objects: []*ascv1.Object{{Key: &ascv1.ObjKey{Kind: req.Keys[0].Kind, Id: []byte{1}}, Data: []byte{0}}}}
		}},
		"an undecodable value": {func(c *Client, req *ascv1.GetObjectsRequest) *ascv1.GetObjectsResponse {
			return &ascv1.GetObjectsResponse{Objects: []*ascv1.Object{{Key: req.Keys[0], Data: []byte{9, 9}}}}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			s := startFake(t)
			c := newTestClient(t, s, nil)
			want, err := c.NewObject(ascv1.ObjKind_OBJ_KIND_FILE, good)
			require.NoError(t, err)
			s.repo.getHook = func(_ int, req *ascv1.GetObjectsRequest, send func(*ascv1.GetObjectsResponse) error) (bool, error) {
				return true, send(tc.hook(c, req))
			}
			_, err = c.GetObjects(WithSystem(t.Context()), "o/r", []*ascv1.ObjKey{want.Key})
			require.ErrorIs(t, err, ErrCorrupt)
			assert.Equal(t, 0, c.CacheStats().Entries, "nothing corrupt is cached")
			assert.Equal(t, 1, s.repo.getCalls, "corruption is not retried")
		})
	}
}

func TestGetObjectsFailsWhenKeysAreNotAccountedFor(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, nil)
	o, err := c.NewObject(ascv1.ObjKind_OBJ_KIND_FILE, []byte("x"))
	require.NoError(t, err)
	s.repo.getHook = func(_ int, _ *ascv1.GetObjectsRequest, send func(*ascv1.GetObjectsResponse) error) (bool, error) {
		return true, send(&ascv1.GetObjectsResponse{}) // neither found nor missing
	}
	_, err = c.GetObjects(WithSystem(t.Context()), "o/r", []*ascv1.ObjKey{o.Key})
	require.ErrorContains(t, err, "did not account")
}

func TestGetObjectsRefusesBadRequests(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, nil)
	ctx := WithSystem(t.Context())
	id := bytes.Repeat([]byte{1}, 32)
	for name, tc := range map[string]struct {
		repo string
		keys []*ascv1.ObjKey
		want error
	}{
		"nil key":      {"o/r", []*ascv1.ObjKey{nil}, ErrBadRequest},
		"short id":     {"o/r", []*ascv1.ObjKey{{Kind: ascv1.ObjKind_OBJ_KIND_FILE, Id: id[:20]}}, ErrBadRequest},
		"chunk list":   {"o/r", []*ascv1.ObjKey{{Kind: ascv1.ObjKind_OBJ_KIND_CHUNK_LIST, Id: id}}, ErrUnsupportedKind},
		"unspecified":  {"o/r", []*ascv1.ObjKey{{Id: id}}, ErrUnsupportedKind},
		"empty repo":   {"", nil, ErrBadRequest},
		"nul in repo":  {"o/\x00r", nil, ErrBadRequest},
		"long repo":    {string(make([]byte, 300)), nil, ErrBadRequest},
		"too many key": {"o/r", make([]*ascv1.ObjKey, MaxKeysPerCall+1), ErrBadRequest},
	} {
		_, err := c.GetObjects(ctx, tc.repo, tc.keys)
		assert.ErrorIs(t, err, tc.want, name)
	}
	assert.Empty(t, s.repo.seenCalls())
}

func TestGetObjectsBatchesLargeRequests(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, nil)
	const n = MaxGetKeys*2 + 5
	keys := make([]*ascv1.ObjKey, 0, n)
	for i := range n {
		o := stock(t, s, c, ascv1.ObjKind_OBJ_KIND_FILE, fmt.Appendf(nil, "file %d", i))
		keys = append(keys, o.Key)
	}
	got, err := c.GetObjects(WithSystem(t.Context()), "o/r", keys)
	require.NoError(t, err)
	for i := range n {
		assert.Equal(t, fmt.Appendf(nil, "file %d", i), got[i])
	}
	require.Len(t, s.repo.getKeys, 3)
	assert.Len(t, s.repo.getKeys[0], MaxGetKeys)
	assert.Len(t, s.repo.getKeys[2], 5)
}

func TestGetObjectsResumesAfterATransientStreamFailure(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, nil)
	primeBudget(c, 100)
	a := stock(t, s, c, ascv1.ObjKind_OBJ_KIND_FILE, []byte("aaa"))
	b := stock(t, s, c, ascv1.ObjKind_OBJ_KIND_FILE, []byte("bbb"))
	s.repo.getHook = func(attempt int, req *ascv1.GetObjectsRequest, send func(*ascv1.GetObjectsResponse) error) (bool, error) {
		if attempt != 1 {
			return false, nil
		}
		// First attempt: deliver one object, then drop.
		if err := send(&ascv1.GetObjectsResponse{Objects: []*ascv1.Object{{Key: a.Key, Data: a.Data}}}); err != nil {
			return true, err
		}
		return true, status.Error(codes.Unavailable, "connection reset")
	}
	got, err := c.GetObjects(WithSystem(t.Context()), "o/r", []*ascv1.ObjKey{a.Key, b.Key})
	require.NoError(t, err)
	assert.Equal(t, [][]byte{[]byte("aaa"), []byte("bbb")}, got)
	require.Len(t, s.repo.getKeys, 2)
	assert.Len(t, s.repo.getKeys[1], 1, "the retry asks only for what is still missing")
	assert.Equal(t, b.Key.Id, s.repo.getKeys[1][0].Id)
}

func TestGetObjectsDoesNotRetryAuthorisationFailures(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, nil)
	primeBudget(c, 100)
	o, _ := c.NewObject(ascv1.ObjKind_OBJ_KIND_FILE, []byte("x"))
	s.repo.getHook = func(int, *ascv1.GetObjectsRequest, func(*ascv1.GetObjectsResponse) error) (bool, error) {
		return true, status.Error(codes.PermissionDenied, "no")
	}
	_, err := c.GetObjects(WithSystem(t.Context()), "o/r", []*ascv1.ObjKey{o.Key})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Equal(t, 1, s.repo.getCalls)
}

func TestHasObjects(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, nil)
	ctx := WithSystem(t.Context())
	key := func(i int) *ascv1.ObjKey {
		return &ascv1.ObjKey{Kind: ascv1.ObjKind_OBJ_KIND_CHUNK_LIST, Id: bytes.Repeat([]byte{byte(i)}, 32)}
	}

	s.repo.hasPresent = []byte{0b0000_0101, 0b0000_0001}
	keys := make([]*ascv1.ObjKey, 9)
	for i := range keys {
		keys[i] = key(i)
	}
	got, err := c.HasObjects(ctx, "o/r", keys)
	require.NoError(t, err)
	assert.Equal(t, []bool{true, false, true, false, false, false, false, false, true}, got, "LSB-first bitmap")

	s.repo.hasPresent = []byte{0xff}
	_, err = c.HasObjects(ctx, "o/r", keys)
	require.ErrorContains(t, err, "wrong length", "a short bitmap is refused")

	// More than 4096 keys go in several requests.
	s.repo.hasPresent = nil
	many := make([]*ascv1.ObjKey, MaxHasKeys+10)
	for i := range many {
		many[i] = key(i)
	}
	got, err = c.HasObjects(ctx, "o/r", many)
	require.NoError(t, err)
	assert.Len(t, got, len(many))
	assert.NotContains(t, got, false)

	got, err = c.HasObjects(ctx, "o/r", nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	for _, bad := range []*ascv1.ObjKey{nil, {}, {Kind: ascv1.ObjKind_OBJ_KIND_FILE}, {Kind: ascv1.ObjKind_OBJ_KIND_FILE, Id: make([]byte, 33)}} {
		_, err = c.HasObjects(ctx, "o/r", []*ascv1.ObjKey{bad})
		assert.ErrorIs(t, err, ErrBadRequest)
	}
}

func TestPutObjectsBatchesAndRetries(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, func(cfg *Config) { cfg.PutBatchBytes = 1000 })
	primeBudget(c, 100)
	var objs []*ascv1.Object
	for i := range 10 {
		// incompressible, so 301 stored bytes plus the 64 byte allowance: two per 1000-byte batch
		o, err := c.NewObject(ascv1.ObjKind_OBJ_KIND_CHUNK, noiseBytes(300, uint32(i+1)))
		require.NoError(t, err)
		objs = append(objs, o)
	}
	s.repo.putFail = []error{status.Error(codes.Unavailable, "restart")}
	stored, already, err := c.PutObjects(WithSystem(t.Context()), "o/r", objs)
	require.NoError(t, err)
	assert.Equal(t, uint64(10), stored)
	assert.Equal(t, uint64(1), already)
	require.Len(t, s.repo.put, 5, "two per batch")
	var total int
	for _, b := range s.repo.put {
		total += len(b)
		assert.Len(t, b, 2)
	}
	assert.Equal(t, 10, total)

	// The server verifies; the client still checks structure before sending anything.
	_, _, err = c.PutObjects(WithSystem(t.Context()), "o/r", []*ascv1.Object{{Key: &ascv1.ObjKey{Kind: ascv1.ObjKind_OBJ_KIND_FILE, Id: []byte{1}}, Data: []byte{0}}})
	assert.ErrorIs(t, err, ErrBadRequest)
	_, _, err = c.PutObjects(WithSystem(t.Context()), "o/r", []*ascv1.Object{nil})
	assert.ErrorIs(t, err, ErrBadRequest)
	huge, err := c.NewObject(ascv1.ObjKind_OBJ_KIND_CHUNK, noiseBytes(5000, 99))
	require.NoError(t, err)
	_, _, err = c.PutObjects(WithSystem(t.Context()), "o/r", []*ascv1.Object{huge})
	assert.ErrorIs(t, err, ErrBadRequest, "an object over the batch limit is refused, not split")

	stored, already, err = c.PutObjects(WithSystem(t.Context()), "o/r", nil)
	require.NoError(t, err)
	assert.Zero(t, stored+already)
}

func noiseBytes(n int, seed uint32) []byte {
	b := make([]byte, n)
	x := 2463534242 + seed
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}

func TestNewObjectRefusesOversizeAndUnverifiableKinds(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, func(cfg *Config) { cfg.MaxDecodedBytes = 100 })
	_, err := c.NewObject(ascv1.ObjKind_OBJ_KIND_FILE, make([]byte, 101))
	assert.ErrorIs(t, err, ErrBadRequest)
	_, err = c.NewObject(ascv1.ObjKind_OBJ_KIND_CHANGE_META, []byte("x"))
	assert.ErrorIs(t, err, ErrUnsupportedKind)
}

func TestObjectCacheEvictsLeastRecentlyUsedByBytes(t *testing.T) {
	c := newObjectCache(10_000)
	key := func(i byte) cacheKey { return cacheKey{kind: ascv1.ObjKind_OBJ_KIND_FILE, id: [32]byte{i}} }
	data := func() []byte { return make([]byte, 1000) }

	for i := range byte(8) {
		c.put(key(i), data())
	}
	_, ok := c.get(key(0))
	require.True(t, ok, "touch 0 so that 1 is the oldest")
	for i := byte(8); i < 12; i++ {
		c.put(key(i), data())
	}
	st := c.stats()
	assert.LessOrEqual(t, st.Bytes, int64(10_000))
	_, ok = c.get(key(0))
	assert.True(t, ok, "recently used survives")
	_, ok = c.get(key(1))
	assert.False(t, ok, "least recently used is evicted")

	// An entry over a quarter of the budget is not cached, so it cannot flush the rest.
	before := c.stats().Entries
	c.put(key(100), make([]byte, 3000))
	assert.Equal(t, before, c.stats().Entries)

	var nilCache *objectCache
	nilCache.put(key(1), data())
	_, ok = nilCache.get(key(1))
	assert.False(t, ok)
	assert.Equal(t, CacheStats{}, nilCache.stats())
	assert.Nil(t, newObjectCache(0))
}
