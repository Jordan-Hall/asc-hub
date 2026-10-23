// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/util"
)

const ascIDLen = 32

// listedRef is one reference as ListRefs reports it, before its ids are turned into git ids.
type listedRef struct {
	name      string
	changeset []byte // asc id of the commit, or nil
	tag       bool   // the ref names an annotated tag
}

// collectRefs pages through ListRefs. It speaks asc ids, the only mode ListRefs has.
func (b *Backend) collectRefs(ctx context.Context, prefix string) ([]listedRef, error) {
	var (
		refs  []listedRef
		after string
	)
	for {
		resp, err := b.api.ListRefs(ctx, &ascv1.ListRefsRequest{Repo: b.ascRef(), Prefix: prefix, After: after, Limit: refPage})
		if err != nil {
			return nil, callErr("ListRefs", prefix, err)
		}
		for _, r := range resp.GetRefs() {
			if len(refs) >= maxRefs {
				return nil, fmt.Errorf("%w: more than %d references", ErrTooLarge, maxRefs)
			}
			name := r.GetName()
			if !strings.HasPrefix(name, "refs/") || strings.ContainsAny(name, "\x00\n") || len(name) > maxRevBytes {
				return nil, badAnswer("reference with an invalid name")
			}
			t := r.GetTarget()
			switch {
			case t.GetChangeset() != nil:
				if len(t.GetChangeset()) != ascIDLen {
					return nil, badAnswer("reference target of %d bytes", len(t.GetChangeset()))
				}
				refs = append(refs, listedRef{name: name, changeset: t.GetChangeset()})
			case t.GetTag() != nil:
				refs = append(refs, listedRef{name: name, tag: true})
			default:
				return nil, badAnswer("reference without a target")
			}
		}
		next := resp.GetNext()
		if next == "" {
			return refs, nil
		}
		if next <= after && after != "" {
			return nil, badAnswer("ListRefs page token does not advance")
		}
		after = next
	}
}

// gitIDsOfChangesets maps asc changeset ids to git commit ids, in order.
func (b *Backend) gitIDsOfChangesets(ctx context.Context, asc [][]byte) ([]git.ObjectID, error) {
	out := make([]git.ObjectID, 0, len(asc))
	for start := 0; start < len(asc); start += maxQueryItems {
		chunk := asc[start:min(start+maxQueryItems, len(asc))]
		resp, err := b.api.GitMap(ctx, &ascv1.GitMapRequest{Repo: b.ascRef(), Asc: chunk})
		if err != nil {
			return nil, callErr("GitMap", "", err)
		}
		if len(resp.GetAscToGit()) != len(chunk) {
			return nil, badAnswer("GitMap answered %d ids for %d", len(resp.GetAscToGit()), len(chunk))
		}
		for _, raw := range resp.GetAscToGit() {
			if len(raw) == 0 {
				return nil, badAnswer("a reference points at a commit with no git id")
			}
			id, err := idFromWire(raw)
			if err != nil {
				return nil, err
			}
			out = append(out, id)
		}
	}
	return out, nil
}

// resolveNames resolves ref names to commit ids (a tag is peeled), in order.
func (b *Backend) resolveNames(ctx context.Context, names []string) ([]git.ObjectID, error) {
	out := make([]git.ObjectID, 0, len(names))
	for start := 0; start < len(names); start += maxQueryItems {
		chunk := names[start:min(start+maxQueryItems, len(names))]
		resp, err := b.api.Resolve(ctx, &ascv1.ResolveRequest{Repo: b.gitRef(), Revspecs: chunk})
		if err != nil {
			return nil, callErr("Resolve", "", err)
		}
		if len(resp.GetIds()) != len(chunk) {
			return nil, badAnswer("Resolve answered %d ids for %d", len(resp.GetIds()), len(chunk))
		}
		for _, raw := range resp.GetIds() {
			if len(raw) == 0 {
				return nil, badAnswer("a reference does not resolve")
			}
			id, err := idFromWire(raw)
			if err != nil {
				return nil, err
			}
			out = append(out, id)
		}
	}
	return out, nil
}

// supportedSort says whether ListRefs can order by key without reading the objects.
func supportedSort(key string) (reverse bool, err error) {
	switch key {
	case "", "refname":
		return false, nil
	case "-refname":
		return true, nil
	}
	if !isSortKey(key) {
		return false, util.NewInvalidArgumentErrorf("invalid ref sort key %q", key)
	}
	return false, fmt.Errorf("%w: ref sort key %q", ErrUnsupported, key)
}

var sortKeyPattern = regexp.MustCompile(`^-?[A-Za-z][A-Za-z0-9_:=.-]{0,127}$`)

func isSortKey(key string) bool { return sortKeyPattern.MatchString(key) }

// ListRefs lists references in ref name order. Remote-tracking refs and refs/stash are skipped. An
// annotated tag is listed as a reference to the commit it peels to (see the package comment).
func (b *Backend) ListRefs(ctx context.Context, q git.RefQuery) ([]git.Reference, error) {
	reverse, err := supportedSort(q.Sort)
	if err != nil {
		return nil, err
	}
	if q.Contains != nil {
		if err := requireSha1(q.Contains); err != nil {
			return nil, err
		}
	}
	ctx, cancel, err := b.stream(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	listed, err := b.collectRefs(ctx, q.Prefix)
	if err != nil {
		return nil, err
	}
	kept := listed[:0]
	for _, r := range listed {
		if strings.HasPrefix(r.name, git.RemotePrefix) || r.name == "refs/stash" || !strings.HasPrefix(r.name, q.Prefix) {
			continue
		}
		kept = append(kept, r)
	}
	listed = kept

	ids, err := b.refIDs(ctx, listed)
	if err != nil {
		return nil, err
	}
	refs := make([]git.Reference, 0, len(listed))
	for i, r := range listed {
		refs = append(refs, git.Reference{Name: r.name, Object: ids[i], Type: string(git.ObjectCommit)})
		b.facts.putCommit(ids[i])
	}
	if reverse {
		slices.Reverse(refs)
	}
	if q.Contains != nil {
		if refs, err = b.refsContaining(ctx, refs, q.Contains, q.Limit); err != nil {
			return nil, err
		}
	}
	if q.Limit > 0 && len(refs) > q.Limit {
		refs = refs[:q.Limit]
	}
	return refs, nil
}

// refIDs gives the git commit id each listed reference points at.
func (b *Backend) refIDs(ctx context.Context, listed []listedRef) ([]git.ObjectID, error) {
	ids := make([]git.ObjectID, len(listed))
	var csIdx []int
	var asc [][]byte
	var tagIdx []int
	var tagNames []string
	for i, r := range listed {
		if r.tag {
			tagIdx = append(tagIdx, i)
			tagNames = append(tagNames, r.name)
		} else {
			csIdx = append(csIdx, i)
			asc = append(asc, r.changeset)
		}
	}
	mapped, err := b.gitIDsOfChangesets(ctx, asc)
	if err != nil {
		return nil, err
	}
	for k, i := range csIdx {
		ids[i] = mapped[k]
	}
	peeled, err := b.resolveNames(ctx, tagNames)
	if err != nil {
		return nil, err
	}
	for k, i := range tagIdx {
		ids[i] = peeled[k]
	}
	return ids, nil
}

// refsContaining keeps the references whose history contains commit. It asks IsAncestor once per
// reference, so it is bounded by maxContainsChecks.
func (b *Backend) refsContaining(ctx context.Context, refs []git.Reference, commit git.ObjectID, limit int) ([]git.Reference, error) {
	var out []git.Reference
	checks := 0
	for _, r := range refs {
		if limit > 0 && len(out) >= limit {
			break
		}
		if checks++; checks > maxContainsChecks {
			return nil, fmt.Errorf("%w: more than %d references to check", ErrTooLarge, maxContainsChecks)
		}
		ok, err := b.IsAncestor(ctx, commit, r.Object)
		if err != nil {
			if git.IsErrNotExist(err) {
				continue
			}
			return nil, err
		}
		if ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// Head returns the reference HEAD points at, as the server reports it.
func (b *Backend) Head(ctx context.Context) (git.RefName, error) {
	ctx, cancel, err := b.unary(ctx)
	if err != nil {
		return "", err
	}
	defer cancel()
	resp, err := b.api.ListRefs(ctx, &ascv1.ListRefsRequest{Repo: b.ascRef(), Prefix: git.BranchPrefix, Limit: 1})
	if err != nil {
		return "", callErr("ListRefs", "HEAD", err)
	}
	head := git.RefName(resp.GetHead())
	if !head.IsBranch() || strings.ContainsAny(string(head), "\x00\n") || len(head) > maxRevBytes {
		return "", fmt.Errorf("%w: the asc server does not report a HEAD branch", ErrUnsupported)
	}
	return head, nil
}

// SetHead is not available: writes go through modules/repostore.
func (b *Backend) SetHead(_ context.Context, _ git.RefName) error {
	return fmt.Errorf("%w: SetHead", ErrUnsupported)
}
