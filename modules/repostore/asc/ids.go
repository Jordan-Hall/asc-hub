// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asc

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
)

const (
	gitIDLen = 20 // bytes of a sha1 id
	ascIDLen = 32 // bytes of a changeset id
)

// requireSha1 refuses an id of another object format.
func requireSha1(id git.ObjectID) error {
	if id == nil || id.Type() != git.Sha1ObjectFormat {
		return fmt.Errorf("%w: only sha1 object ids are supported", repostore.ErrUnsupported)
	}
	return nil
}

func idFromWire(b []byte) (git.ObjectID, error) {
	if len(b) != gitIDLen {
		return nil, fmt.Errorf("%w: object id of %d bytes", ErrBadAnswer, len(b))
	}
	return git.Sha1ObjectFormat.MustID(b), nil
}

// gitToAsc converts git commit ids of repo to changeset ids, in the order given. An id that asc does not
// know is a *repostore.RefNotFoundError.
func (s *session) gitToAsc(h *repoHandle, ids ...git.ObjectID) ([][]byte, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > MaxMapIDs {
		return nil, fmt.Errorf("%w: %d ids to map", ErrTooLarge, len(ids))
	}
	wire := make([][]byte, len(ids))
	for i, id := range ids {
		if err := requireSha1(id); err != nil {
			return nil, err
		}
		wire[i] = id.RawValue()
	}
	resp, err := s.api.GitMap(s.ctx, &ascv1.GitMapRequest{Repo: h.ascRef(), Git: wire})
	if err != nil {
		return nil, callErr("GitMap", err)
	}
	got := resp.GetGitToAsc()
	if len(got) != len(ids) {
		return nil, fmt.Errorf("%w: GitMap answered %d ids for %d", ErrBadAnswer, len(got), len(ids))
	}
	for i, c := range got {
		switch len(c) {
		case 0:
			return nil, refMissing(ids[i].String())
		case ascIDLen:
		default:
			return nil, fmt.Errorf("%w: changeset id of %d bytes", ErrBadAnswer, len(c))
		}
	}
	return got, nil
}

// ascToGit converts changeset ids of repo to git commit ids, in the order given.
func (s *session) ascToGit(h *repoHandle, ids ...[]byte) ([]git.ObjectID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > MaxMapIDs {
		return nil, fmt.Errorf("%w: %d ids to map", ErrTooLarge, len(ids))
	}
	for _, id := range ids {
		if len(id) != ascIDLen {
			return nil, fmt.Errorf("%w: changeset id of %d bytes", ErrBadAnswer, len(id))
		}
	}
	resp, err := s.api.GitMap(s.ctx, &ascv1.GitMapRequest{Repo: h.ascRef(), Asc: ids})
	if err != nil {
		return nil, callErr("GitMap", err)
	}
	got := resp.GetAscToGit()
	if len(got) != len(ids) {
		return nil, fmt.Errorf("%w: GitMap answered %d ids for %d", ErrBadAnswer, len(got), len(ids))
	}
	out := make([]git.ObjectID, len(got))
	for i, g := range got {
		if len(g) == 0 {
			return nil, refMissing(hex.EncodeToString(ids[i]))
		}
		if out[i], err = idFromWire(g); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// gitAndAsc is a commit in both id spaces.
type gitAndAsc struct {
	git git.ObjectID
	asc []byte
}

// pair maps one git commit id to its changeset id.
func (s *session) pair(h *repoHandle, id git.ObjectID) (gitAndAsc, error) {
	asc, err := s.gitToAsc(h, id)
	if err != nil {
		return gitAndAsc{}, err
	}
	return gitAndAsc{git: id, asc: asc[0]}, nil
}

// validRev reports whether rev can be sent as a revision.
func validRev(rev string) bool {
	return rev != "" && len(rev) <= MaxRevBytes && !strings.ContainsAny(rev, "\n\x00") && utf8.ValidString(rev)
}

// resolve returns the git commit a revision names in repo. A full git id is returned as it is (the
// next GitMap call checks that asc has it); anything else is resolved by asc. It returns nil, nil when the
// revision names nothing.
func (s *session) resolve(h *repoHandle, rev string) (git.ObjectID, error) {
	if !validRev(rev) {
		return nil, fmt.Errorf("%w: revision is not usable", repostore.ErrInvalidRequest)
	}
	if id, err := git.NewIDFromString(rev); err == nil {
		if err := requireSha1(id); err != nil {
			return nil, err
		}
		return id, nil
	}
	resp, err := s.api.Resolve(s.ctx, &ascv1.ResolveRequest{Repo: h.gitRef(), Revspecs: []string{rev}})
	if err != nil {
		return nil, callErr("Resolve", err)
	}
	ids := resp.GetIds()
	if len(ids) != 1 {
		return nil, fmt.Errorf("%w: Resolve answered %d ids for 1", ErrBadAnswer, len(ids))
	}
	if len(ids[0]) == 0 {
		return nil, nil
	}
	return idFromWire(ids[0])
}

// mustResolve is resolve for a revision that has to exist.
func (s *session) mustResolve(h *repoHandle, rev string) (git.ObjectID, error) {
	id, err := s.resolve(h, rev)
	if err != nil {
		return nil, err
	}
	if id == nil {
		return nil, refMissing(rev)
	}
	return id, nil
}

// copyClosure makes the closure of the changesets in from available in to. It does nothing for one repository.
func (s *session) copyClosure(from, to *repoHandle, ascIDs ...[]byte) error {
	if same(from, to) || len(ascIDs) == 0 {
		return nil
	}
	_, err := s.api.CopyClosure(s.ctx, &ascv1.CopyClosureRequest{From: from.ascRef(), To: to.ascRef(), Changesets: ascIDs})
	return callErr("CopyClosure", err)
}

// wireSignature converts a signature. A zero time means now.
func wireSignature(sig *repostore.Signature, now time.Time) *ascv1.Signature {
	if sig == nil {
		return nil
	}
	when := sig.When
	if when.IsZero() {
		when = now
	}
	_, offset := when.Zone()
	return &ascv1.Signature{
		Name:  []byte(sig.Name),
		Email: []byte(sig.Email),
		Time:  when.Unix(),
		TzMin: int32(offset / 60),
	}
}

// fromWireSignature converts the signature of a log entry.
func fromWireSignature(sig *ascv1.Signature) *repostore.Signature {
	if sig == nil {
		return nil
	}
	return &repostore.Signature{
		Name:  string(sig.GetName()),
		Email: string(sig.GetEmail()),
		When:  time.Unix(sig.GetTime(), 0).In(time.FixedZone("", int(sig.GetTzMin())*60)),
	}
}

// short gives an id for an error message without the whole value.
func short(id git.ObjectID) string {
	if id == nil {
		return "<nil>"
	}
	s := id.String()
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
