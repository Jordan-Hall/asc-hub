// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/klauspost/compress/zstd"
	"github.com/zeebo/blake3"
)

// Stored value codecs (SPEC section 3.2): `codec: u8 || data`.
const (
	codecRaw  = 0
	codecZstd = 1

	// maxBatchDecodedBytes bounds the decoded bytes one GetObjects exchange may deliver.
	maxBatchDecodedBytes = 512 << 20
	maxRepoNameBytes     = 255
	// zstdMaxWindow bounds decoder memory per frame; asc writes level-3 frames (2 MiB window).
	zstdMaxWindow = 32 << 20
	// zstdMinGain: a writer uses zstd only if the frame is under 90% of the raw size.
	zstdMinGainPercent = 90
)

var (
	// ErrCorrupt means an object did not match its id, or its encoding was invalid. The caller
	// must treat the object as missing; nothing corrupt is ever cached or returned.
	ErrCorrupt = errors.New("ascclient: object failed verification")
	// ErrUnsupportedKind means a kind whose id cannot be checked from its own bytes (ChunkList,
	// ChangeMeta, GitTreeOverride). Use the raw Repo() stubs for those.
	ErrUnsupportedKind = errors.New("ascclient: object kind is not verifiable here")
	// ErrBadRequest marks a request the client refuses to send.
	ErrBadRequest = errors.New("ascclient: bad request")
)

// idContext maps a verifiable kind to the BLAKE3 derive-key context of its id (SPEC section 2.1).
var idContext = map[ascv1.ObjKind]string{
	ascv1.ObjKind_OBJ_KIND_FILE:      "asc v1 file",
	ascv1.ObjKind_OBJ_KIND_CHUNK:     "asc v1 chunk",
	ascv1.ObjKind_OBJ_KIND_TREE:      "asc v1 tree",
	ascv1.ObjKind_OBJ_KIND_CHANGESET: "asc v1 changeset",
	ascv1.ObjKind_OBJ_KIND_TAG:       "asc v1 tag",
}

// ObjectID returns the id of data as an object of the given kind.
func ObjectID(kind ascv1.ObjKind, data []byte) ([]byte, error) {
	ctx, ok := idContext[kind]
	if !ok {
		return nil, ErrUnsupportedKind
	}
	h := blake3.NewDeriveKey(ctx)
	_, _ = h.Write(data)
	return h.Sum(nil), nil
}

func checkRepo(repo string) error {
	if repo == "" || len(repo) > maxRepoNameBytes || strings.ContainsRune(repo, 0) {
		return fmt.Errorf("%w: repository name", ErrBadRequest)
	}
	return nil
}

func toCacheKey(scope [16]byte, repo string, k *ascv1.ObjKey) (cacheKey, error) {
	if k == nil {
		return cacheKey{}, fmt.Errorf("%w: nil key", ErrBadRequest)
	}
	if _, ok := idContext[k.GetKind()]; !ok {
		return cacheKey{}, ErrUnsupportedKind
	}
	if len(k.GetId()) != 32 {
		return cacheKey{}, fmt.Errorf("%w: object id must be 32 bytes", ErrBadRequest)
	}
	ck := cacheKey{scope: scope, repo: repo, kind: k.GetKind()}
	copy(ck.id[:], k.GetId())
	return ck, nil
}

// scopeOf names the principal of a call for the cache. User tokens are hashed, so no token is
// held by the cache.
func scopeOf(ctx context.Context) [16]byte {
	var s [16]byte
	cred, _ := ctx.Value(credKey{}).(credential)
	sum := sha256.Sum256([]byte(map[bool]string{true: "system", false: "user\x00" + cred.token}[cred.system]))
	copy(s[:], sum[:])
	return s
}

// decodeStored removes the codec byte. A zstd frame must be a single frame that declares its
// content size, and that size is checked against max before anything is allocated.
func (c *Client) decodeStored(stored []byte) ([]byte, error) {
	if len(stored) == 0 {
		return nil, fmt.Errorf("%w: empty value", ErrCorrupt)
	}
	body := stored[1:]
	switch stored[0] {
	case codecRaw:
		if int64(len(body)) > c.cfg.MaxDecodedBytes {
			return nil, fmt.Errorf("%w: object exceeds %d bytes", ErrCorrupt, c.cfg.MaxDecodedBytes)
		}
		return body, nil
	case codecZstd:
		var h zstd.Header
		if err := h.Decode(body); err != nil || h.Skippable || !h.HasFCS {
			return nil, fmt.Errorf("%w: bad zstd frame", ErrCorrupt)
		}
		if h.FrameContentSize > uint64(c.cfg.MaxDecodedBytes) {
			return nil, fmt.Errorf("%w: object exceeds %d bytes", ErrCorrupt, c.cfg.MaxDecodedBytes)
		}
		out, err := c.dec.DecodeAll(body, make([]byte, 0, h.FrameContentSize))
		if err != nil || uint64(len(out)) != h.FrameContentSize {
			return nil, fmt.Errorf("%w: bad zstd frame", ErrCorrupt)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%w: unknown codec %d", ErrCorrupt, stored[0])
	}
}

// encodeStored is the writer side of decodeStored: zstd only if the frame is under 90% of raw.
func (c *Client) encodeStored(data []byte) []byte {
	if len(data) >= 64 {
		if z := c.enc.EncodeAll(data, make([]byte, 0, len(data)/2+16)); len(z)*100 < len(data)*zstdMinGainPercent {
			return append([]byte{codecZstd}, z...)
		}
	}
	return append([]byte{codecRaw}, data...)
}

// verify checks data against the id in key.
func verify(key cacheKey, data []byte) error {
	want, err := ObjectID(key.kind, data)
	if err != nil || subtle.ConstantTimeCompare(want, key.id[:]) != 1 {
		return ErrCorrupt
	}
	return nil
}

// NewObject builds a wire object from raw data: its id is computed, and the data is stored
// under the codec rule above. For Tree, Changeset and Tag, data is the canonical envelope.
func (c *Client) NewObject(kind ascv1.ObjKind, data []byte) (*ascv1.Object, error) {
	if int64(len(data)) > c.cfg.MaxDecodedBytes {
		return nil, fmt.Errorf("%w: object exceeds %d bytes", ErrBadRequest, c.cfg.MaxDecodedBytes)
	}
	id, err := ObjectID(kind, data)
	if err != nil {
		return nil, err
	}
	return &ascv1.Object{Key: &ascv1.ObjKey{Kind: kind, Id: id}, Data: c.encodeStored(data)}, nil
}

// HasObjects reports which keys the repository holds, in key order. It batches by 4096 keys.
func (c *Client) HasObjects(ctx context.Context, repo string, keys []*ascv1.ObjKey) ([]bool, error) {
	if err := checkRepo(repo); err != nil {
		return nil, err
	}
	if len(keys) > MaxKeysPerCall {
		return nil, fmt.Errorf("%w: more than %d keys", ErrBadRequest, MaxKeysPerCall)
	}
	for _, k := range keys {
		if k == nil || k.GetKind() == ascv1.ObjKind_OBJ_KIND_UNSPECIFIED || len(k.GetId()) == 0 || len(k.GetId()) > 32 {
			return nil, fmt.Errorf("%w: bad key", ErrBadRequest)
		}
	}
	out := make([]bool, 0, len(keys))
	for start := 0; start < len(keys); start += MaxHasKeys {
		batch := keys[start:min(start+MaxHasKeys, len(keys))]
		resp, err := c.Repo().HasObjects(ctx, &ascv1.HasObjectsRequest{Repo: &ascv1.RepoRef{Name: repo}, Keys: batch})
		if err != nil {
			return nil, err
		}
		if len(resp.GetPresent()) != (len(batch)+7)/8 {
			return nil, errors.New("ascclient: HasObjects bitmap has the wrong length")
		}
		for i := range batch {
			out = append(out, resp.GetPresent()[i/8]>>(i%8)&1 == 1)
		}
	}
	return out, nil
}

// GetObjects returns the decoded data of keys, in key order; a missing object is nil. Every
// object is checked against its id before it is returned or cached, so a corrupt or swapped
// object from a hostile or damaged server fails with ErrCorrupt. Only the five content-addressed
// kinds are accepted (ErrUnsupportedKind otherwise), and keys must be 32-byte asc ids.
//
// The returned slices are shared with the cache: do not modify them.
func (c *Client) GetObjects(ctx context.Context, repo string, keys []*ascv1.ObjKey) ([][]byte, error) {
	if err := checkRepo(repo); err != nil {
		return nil, err
	}
	if len(keys) > MaxKeysPerCall {
		return nil, fmt.Errorf("%w: more than %d keys", ErrBadRequest, MaxKeysPerCall)
	}
	if _, err := c.bearer(ctx); err != nil {
		return nil, err
	}
	scope := scopeOf(ctx)
	out := make([][]byte, len(keys))
	positions := make(map[cacheKey][]int)
	var misses []cacheKey
	for i, k := range keys {
		ck, err := toCacheKey(scope, repo, k)
		if err != nil {
			return nil, err
		}
		if data, ok := c.cache.get(ck); ok {
			out[i] = data
			continue
		}
		if _, dup := positions[ck]; !dup {
			misses = append(misses, ck)
		}
		positions[ck] = append(positions[ck], i)
	}
	for start := 0; start < len(misses); start += MaxGetKeys {
		found, err := c.fetch(ctx, repo, misses[start:min(start+MaxGetKeys, len(misses))])
		if err != nil {
			return nil, err
		}
		for ck, data := range found {
			for _, i := range positions[ck] {
				out[i] = data
			}
		}
	}
	return out, nil
}

// fetch runs one GetObjects exchange for at most MaxGetKeys keys. A failed attempt keeps the
// objects it already verified and asks again only for the rest.
func (c *Client) fetch(ctx context.Context, repo string, batch []cacheKey) (map[cacheKey][]byte, error) {
	pending := make(map[cacheKey]struct{}, len(batch))
	for _, k := range batch {
		pending[k] = struct{}{}
	}
	found := make(map[cacheKey][]byte, len(batch))
	var decoded int64
	err := c.retry.do(ctx, true, func(ctx context.Context) (time.Duration, error) {
		actx, cancel := context.WithTimeout(ctx, c.cfg.StreamTimeout)
		defer cancel()
		stream, err := c.Repo().GetObjects(actx)
		if err != nil {
			return 0, err
		}
		req := &ascv1.GetObjectsRequest{Repo: &ascv1.RepoRef{Name: repo}}
		for _, k := range batch {
			if _, ok := pending[k]; ok {
				req.Keys = append(req.Keys, &ascv1.ObjKey{Kind: k.kind, Id: bytes.Clone(k.id[:])})
			}
		}
		if err := stream.Send(req); err != nil {
			if errors.Is(err, io.EOF) { // the call already failed; the status is on Recv
				if _, err = stream.Recv(); err == nil {
					err = io.ErrUnexpectedEOF
				}
			}
			return retryHint(stream.Trailer()), err
		}
		if err := stream.CloseSend(); err != nil {
			return 0, err
		}
		for {
			resp, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return retryHint(stream.Trailer()), err
			}
			if err := c.accept(resp, repo, batch[0].scope, pending, found, &decoded); err != nil {
				return 0, err
			}
		}
		if len(pending) > 0 {
			return 0, errors.New("ascclient: GetObjects response did not account for every key")
		}
		return 0, nil
	})
	return found, err
}

// accept verifies one response message and moves its objects from pending to found.
func (c *Client) accept(resp *ascv1.GetObjectsResponse, repo string, scope [16]byte, pending map[cacheKey]struct{}, found map[cacheKey][]byte, decoded *int64) error {
	for _, o := range resp.GetObjects() {
		ck, err := toCacheKey(scope, repo, o.GetKey())
		if err != nil {
			return fmt.Errorf("%w: server returned an unexpected key", ErrCorrupt)
		}
		if _, ok := pending[ck]; !ok {
			return fmt.Errorf("%w: server returned an object that was not requested", ErrCorrupt)
		}
		data, err := c.decodeStored(o.GetData())
		if err != nil {
			return err
		}
		if *decoded += int64(len(data)); *decoded > maxBatchDecodedBytes {
			return fmt.Errorf("%w: response exceeds %d bytes", ErrCorrupt, int64(maxBatchDecodedBytes))
		}
		if err := verify(ck, data); err != nil {
			return err
		}
		if data == nil {
			data = []byte{}
		}
		delete(pending, ck)
		found[ck] = data
		c.cache.put(ck, data)
	}
	for _, k := range resp.GetMissing() {
		ck, err := toCacheKey(scope, repo, k)
		if err != nil {
			return fmt.Errorf("%w: server returned an unexpected key", ErrCorrupt)
		}
		if _, ok := pending[ck]; !ok {
			return fmt.Errorf("%w: server reported an unrequested key missing", ErrCorrupt)
		}
		delete(pending, ck)
	}
	return nil
}

// PutObjects uploads objects (build them with NewObject) and returns the server's counts of newly
// stored and already present objects. Objects go in messages of at most Config.PutBatchBytes. The
// upload is idempotent (create-only puts), so a transient failure replays the whole stream.
func (c *Client) PutObjects(ctx context.Context, repo string, objs []*ascv1.Object) (stored, already uint64, err error) {
	if err := checkRepo(repo); err != nil {
		return 0, 0, err
	}
	if len(objs) > MaxKeysPerCall {
		return 0, 0, fmt.Errorf("%w: more than %d objects", ErrBadRequest, MaxKeysPerCall)
	}
	var batches [][]*ascv1.Object
	var cur []*ascv1.Object
	size := 0
	for _, o := range objs {
		k := o.GetKey()
		if _, ok := idContext[k.GetKind()]; !ok || len(k.GetId()) != 32 || len(o.GetData()) == 0 {
			return 0, 0, fmt.Errorf("%w: malformed object", ErrBadRequest)
		}
		n := len(o.GetData()) + 64
		if n > c.cfg.PutBatchBytes {
			return 0, 0, fmt.Errorf("%w: object larger than the %d byte batch limit", ErrBadRequest, c.cfg.PutBatchBytes)
		}
		if len(cur) > 0 && (size+n > c.cfg.PutBatchBytes || len(cur) >= MaxPutObjects) {
			batches, cur, size = append(batches, cur), nil, 0
		}
		cur, size = append(cur, o), size+n
	}
	if len(cur) > 0 {
		batches = append(batches, cur)
	}
	if len(batches) == 0 {
		return 0, 0, nil
	}
	if _, err := c.bearer(ctx); err != nil {
		return 0, 0, err
	}
	err = c.retry.do(ctx, true, func(ctx context.Context) (time.Duration, error) {
		actx, cancel := context.WithTimeout(ctx, c.cfg.StreamTimeout)
		defer cancel()
		stream, err := c.Repo().PutObjects(actx)
		if err != nil {
			return 0, err
		}
		for _, b := range batches {
			if err := stream.Send(&ascv1.PutObjectsRequest{Repo: &ascv1.RepoRef{Name: repo}, Objects: b}); err != nil {
				if errors.Is(err, io.EOF) {
					_, err = stream.CloseAndRecv()
				}
				return retryHint(stream.Trailer()), err
			}
		}
		resp, err := stream.CloseAndRecv()
		if err != nil {
			return retryHint(stream.Trailer()), err
		}
		stored, already = resp.GetStored(), resp.GetAlreadyPresent()
		return 0, nil
	})
	return stored, already, err
}
