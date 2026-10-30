// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// The secrets a workflow's command step reads (`secrets = { TOKEN = "name" }`), kept by asc, repository scoped. The page lists names
// and hints only. A value goes to asc once, when it is set, and comes back nowhere: not in an answer, not in an error, not in a log.

const (
	// MaxWorkflowSecretValue is the longest value asc takes.
	MaxWorkflowSecretValue = 16384
	// MaxWorkflowSecrets bounds the rows read.
	MaxWorkflowSecrets = 500
)

var workflowSecretName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// WorkflowSecretRow is one secret as the page shows it.
type WorkflowSecretRow struct {
	Name      string
	Hint      string // the last characters, when the value is long enough that they reveal nothing
	Updated   time.Time
	UpdatedBy string
}

func workflowSecretRowOf(s *ascv1.WorkflowSecret) (WorkflowSecretRow, bool) {
	if s == nil || !workflowSecretName.MatchString(s.GetName()) {
		return WorkflowSecretRow{}, false
	}
	row := WorkflowSecretRow{Name: s.GetName(), Hint: CleanText(s.GetHint(), 8), UpdatedBy: CleanText(s.GetUpdatedBy(), 80)}
	if s.GetUpdated() > 0 {
		row.Updated = time.Unix(s.GetUpdated(), 0)
	}
	return row, true
}

// ListWorkflowSecrets lists the names of the secrets of a repository.
func ListWorkflowSecrets(ctx context.Context, conn grpc.ClientConnInterface, repoName string) ([]WorkflowSecretRow, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewWorkflowsClient(conn).ListSecrets(cctx, &ascv1.ListWorkflowSecretsRequest{Repo: RepoRef(repoName)})
	if err != nil {
		return nil, err
	}
	var out []WorkflowSecretRow
	for i, s := range resp.GetSecrets() {
		if i >= MaxWorkflowSecrets {
			break
		}
		if row, ok := workflowSecretRowOf(s); ok {
			out = append(out, row)
		}
	}
	return out, nil
}

// SetWorkflowSecret stores a secret. The name and the value are checked before anything is sent. Nothing returned or wrapped here
// carries the value.
func SetWorkflowSecret(ctx context.Context, conn grpc.ClientConnInterface, repoName, name, value, op string) (*WorkflowSecretRow, error) {
	if !workflowSecretName.MatchString(name) {
		return nil, fmt.Errorf("%w: a secret name is 1 to 64 letters, digits, dots, dashes or underscores", ErrBadInput)
	}
	if value == "" || len(value) > MaxWorkflowSecretValue {
		return nil, fmt.Errorf("%w: a secret value is 1 to %d bytes", ErrBadInput, MaxWorkflowSecretValue)
	}
	opID, err := uuid.Parse(op)
	if err != nil {
		return nil, fmt.Errorf("%w: operation id", ErrBadInput)
	}
	cctx, cancel := context.WithTimeout(ascclient.WithOpID(ctx, opID), CallTimeout)
	defer cancel()
	resp, err := ascv1.NewWorkflowsClient(conn).SetSecret(cctx, &ascv1.SetWorkflowSecretRequest{Repo: RepoRef(repoName), Name: name, Value: value})
	if err != nil {
		return nil, err
	}
	row, ok := workflowSecretRowOf(resp.GetSecret())
	if !ok || row.Name != name {
		return nil, fmt.Errorf("%w: secret", ErrBadAnswer)
	}
	return &row, nil
}

// DeleteWorkflowSecret removes a secret.
func DeleteWorkflowSecret(ctx context.Context, conn grpc.ClientConnInterface, repoName, name, op string) error {
	if !workflowSecretName.MatchString(name) {
		return fmt.Errorf("%w: secret name", ErrBadInput)
	}
	opID, err := uuid.Parse(op)
	if err != nil {
		return fmt.Errorf("%w: operation id", ErrBadInput)
	}
	cctx, cancel := context.WithTimeout(ascclient.WithOpID(ctx, opID), CallTimeout)
	defer cancel()
	_, err = ascv1.NewWorkflowsClient(conn).DeleteSecret(cctx, &ascv1.DeleteWorkflowSecretRequest{Repo: RepoRef(repoName), Name: name})
	return err
}
