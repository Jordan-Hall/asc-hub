// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascsync

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
)

// ImportedHistoryNote is the note a secret found in imported history carries on the secret scanning page.
const ImportedHistoryNote = "found in imported history"

const (
	// MaxScannedBranches bounds the branches whose tip is scanned after an import.
	MaxScannedBranches = 20
	// maxNotedFindings bounds the findings that get the note after one import.
	maxNotedFindings = 200
)

var (
	secretMu  sync.Mutex
	secretVal ascv1.SecretScanClient

	// newSecretAPI builds the SecretScan client. Tests replace it through UseSecretClient.
	newSecretAPI = func() (ascv1.SecretScanClient, error) {
		if setting.InternalToken == "" {
			return nil, fmt.Errorf("ascsync: the internal token is not set")
		}
		c, err := ascclient.New(ascclient.Config{
			Target:        setting.Asc.ServerURL,
			InternalToken: setting.InternalToken,
			CallTimeout:   BulkImportTimeout,
		})
		if err != nil {
			return nil, err
		}
		return ascv1.NewSecretScanClient(c), nil
	}
)

// UseSecretClient replaces the SecretScan client until the returned function is called. It is for tests.
func UseSecretClient(api ascv1.SecretScanClient) (restore func()) {
	secretMu.Lock()
	defer secretMu.Unlock()
	prev := secretVal
	secretVal = api
	return func() {
		secretMu.Lock()
		defer secretMu.Unlock()
		secretVal = prev
	}
}

func secretAPI() (ascv1.SecretScanClient, error) {
	secretMu.Lock()
	defer secretMu.Unlock()
	if secretVal == nil {
		api, err := newSecretAPI()
		if err != nil {
			return nil, fmt.Errorf("ascsync: asc client: %w", err)
		}
		secretVal = api
	}
	return secretVal, nil
}

// ScanImportedHistory scans the tips of branches (the default branch first, then the others, at most
// MaxScannedBranches) of an asc repository after its history was imported, and marks what it finds with
// ImportedHistoryNote so the secret scanning page shows where it came from. An import skips the pre-update gate, so
// this is where the history is looked at. It never blocks: findings are recorded as open, and the number of findings
// is returned. A scan that fails or stops at its limits is an error the caller logs; the import stays.
func ScanImportedHistory(ctx context.Context, repo *repo_model.Repository, defaultBranch string, branches []string) (int, error) {
	h, err := open(repo)
	if err != nil {
		return 0, err
	}
	api, err := secretAPI()
	if err != nil {
		return 0, err
	}
	order := slices.Clone(branches)
	slices.Sort(order)
	if i := slices.Index(order, defaultBranch); i > 0 {
		order = slices.Insert(slices.Delete(order, i, i+1), 0, defaultBranch)
	}
	if len(order) > MaxScannedBranches {
		order = order[:MaxScannedBranches]
	}

	seen := map[uint64]bool{}
	var firstErr error
	var ids []uint64
	for _, b := range order {
		cctx, cancel := system(ctx, BulkImportTimeout)
		resp, err := api.Scan(cctx, &ascv1.ScanSecretsRequest{
			Repo: &ascv1.RepoRef{Name: h.name, IdMode: ascv1.IdMode_ID_MODE_ASC},
			At:   &ascv1.ScanSecretsRequest_Rev{Rev: "refs/heads/" + strings.TrimPrefix(b, "refs/heads/")},
		})
		cancel()
		if err != nil {
			if firstErr == nil {
				firstErr = callError("secret scan", err)
			}
			continue
		}
		if resp.GetTruncated() && firstErr == nil {
			firstErr = fmt.Errorf("ascsync: the secret scan of %s stopped at its limits", b)
		}
		for _, f := range resp.GetFindings() {
			if !seen[f.GetId()] {
				seen[f.GetId()] = true
				if f.GetNote() == "" && f.GetState() == ascv1.SecretFindingState_SECRET_FINDING_STATE_OPEN {
					ids = append(ids, f.GetId())
				}
			}
		}
	}
	for i, id := range ids {
		if i >= maxNotedFindings {
			break
		}
		cctx, cancel := system(ctx, CallTimeout)
		_, err := api.UpdateFinding(cctx, &ascv1.UpdateSecretFindingRequest{
			Repo:  &ascv1.RepoRef{Name: h.name, IdMode: ascv1.IdMode_ID_MODE_ASC},
			Id:    id,
			State: ascv1.SecretFindingState_SECRET_FINDING_STATE_OPEN,
			Note:  ImportedHistoryNote,
		})
		cancel()
		if err != nil && firstErr == nil {
			firstErr = callError("note a secret finding", err)
		}
	}
	return len(seen), firstErr
}

// SetDefaultBranch makes branch the trunk of an asc repository: the ref asc's gate protects with its path rules,
// secret scanning and review rules. asc takes the trunk from the repository when it is made; a repository whose
// history is imported afterwards, or whose default branch is changed in the hub, has to tell asc, or the branch
// people push to would be an ordinary, ungated one.
func SetDefaultBranch(ctx context.Context, repo *repo_model.Repository, branch string) error {
	h, err := open(repo)
	if err != nil {
		return err
	}
	if branch == "" || strings.ContainsAny(branch, "\x00\r\n") {
		return fmt.Errorf("%w: default branch", ErrBadRequest)
	}
	cctx, cancel := system(ctx, CallTimeout)
	defer cancel()
	if _, err := h.api.RepoAdmin(cctx, &ascv1.RepoAdminRequest{
		Repo: &ascv1.RepoRef{Name: h.name, IdMode: ascv1.IdMode_ID_MODE_ASC},
		Op:   &ascv1.RepoAdminRequest_SetDefaultBranch{SetDefaultBranch: branch},
	}); err != nil {
		return callError("set the default branch", err)
	}
	return nil
}

// TrunkOf returns the default branch asc protects for the repository, as asc reports it (the HEAD of ListRefs). An
// asc that does not report one gives "" and no error: the caller cannot tell.
func TrunkOf(ctx context.Context, repo *repo_model.Repository) (string, error) {
	h, err := open(repo)
	if err != nil {
		return "", err
	}
	cctx, cancel := system(ctx, CallTimeout)
	defer cancel()
	resp, err := h.api.ListRefs(cctx, &ascv1.ListRefsRequest{
		Repo:   &ascv1.RepoRef{Name: h.name, IdMode: ascv1.IdMode_ID_MODE_ASC},
		Prefix: "refs/heads/",
		Limit:  1,
	})
	if err != nil {
		return "", callError("list refs", err)
	}
	if resp.GetHead() == "" {
		return "", nil
	}
	head, ok := strings.CutPrefix(resp.GetHead(), "refs/heads/")
	if !ok || head == "" {
		return "", fmt.Errorf("%w: asc reports the default branch %q", ErrBadAnswer, resp.GetHead())
	}
	return head, nil
}
