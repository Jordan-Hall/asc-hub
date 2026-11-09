// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascagentreport takes the abuse reports an agent sends with its agent token (board AdminModeration, "agent
// reporters"). An agent that reviews a repository can flag a spam issue or a comment that carries malware links the
// same way a person does with the report form; the report lands in the same moderation queue, marked as sent by the
// agent.
//
// Trust. The agent token is the only credential. The hub does not know agent tokens: it sends the secret to asc
// (AgentTokens.ResolveToken, a call only the host service may make) and gets back the token's repository, agent and
// state, never the secret. A token is accepted only while it is active and only for content of the repository it was
// made for. Every refusal of a token looks the same, so a caller learns nothing about tokens it does not hold. One
// token has at most one open report per content and at most moderation.MaxOpenAgentReports open reports.
package ascagentreport

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"unicode"

	"forgejo.org/models/issues"
	"forgejo.org/models/moderation"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascmoderation"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The errors of Report. Callers map them to statuses; their text is safe to show.
var (
	// ErrDisabled: moderation or the asc integration is off.
	ErrDisabled = errors.New("agent reports are not enabled")
	// ErrToken: the secret is malformed, unknown, not active, or made for another repository.
	ErrToken = errors.New("the agent token is not valid for this repository")
	// ErrNotFound: the content is not in the repository of the token.
	ErrNotFound = errors.New("the content was not found in this repository")
	// ErrInvalid: the report itself is malformed.
	ErrInvalid = errors.New("the report is not valid")
	// ErrUnavailable: asc could not be asked.
	ErrUnavailable = errors.New("asc is not available right now")
)

// Content kinds an agent may report. An agent works inside one repository, so it cannot report an account.
const (
	ContentRepository = "repository"
	ContentIssue      = "issue"
	ContentComment    = "comment"
)

// MaxRemarks bounds the remarks, like the report form of a person.
const MaxRemarks = 500

// maxAgentName bounds the agent name kept with a report (the column is 64 bytes).
const maxAgentName = 64

var secretRe = regexp.MustCompile(`^ascat_[A-Za-z0-9_-]{43}$`)

// categories maps the names of the API to the categories of the report form.
var categories = map[string]moderation.AbuseCategoryType{
	"spam":            moderation.AbuseCategoryTypeSpam,
	"malware":         moderation.AbuseCategoryTypeMalware,
	"illegal_content": moderation.AbuseCategoryTypeIllegalContent,
	"other":           moderation.AbuseCategoryTypeOther,
}

// Request is one report as the agent sends it.
type Request struct {
	// Content is repository, issue or comment.
	Content string `json:"content"`
	// Number is the number of the issue or pull request (content issue).
	Number int64 `json:"number,omitempty"`
	// CommentID is the id of the comment (content comment).
	CommentID int64 `json:"comment_id,omitempty"`
	// Category is spam, malware, illegal_content or other.
	Category string `json:"category"`
	// Remarks is what the agent saw, at most MaxRemarks characters.
	Remarks string `json:"remarks"`
}

// Result is the answer to a report.
type Result struct {
	ID int64 `json:"id"`
	// Duplicate is true when the token already had an open report on the content; ID is that report.
	Duplicate bool `json:"duplicate"`
}

var (
	connMu  sync.Mutex
	connVal grpc.ClientConnInterface
	// newConn builds the connection of this process; tests replace it.
	newConn = func() (grpc.ClientConnInterface, error) {
		if !setting.Asc.Enabled || setting.Asc.ServerURL == "" {
			return nil, ErrDisabled
		}
		if setting.InternalToken == "" {
			return nil, errors.New("ascagentreport: the internal token is not set")
		}
		return ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken})
	}
)

func conn() (grpc.ClientConnInterface, error) {
	connMu.Lock()
	defer connMu.Unlock()
	if connVal == nil {
		c, err := newConn()
		if err != nil {
			return nil, err
		}
		connVal = c
	}
	return connVal, nil
}

// UseConn replaces the connection of this process and returns a function that restores the old one (tests).
func UseConn(c grpc.ClientConnInterface) (restore func()) {
	connMu.Lock()
	old := connVal
	connVal = c
	connMu.Unlock()
	return func() {
		connMu.Lock()
		connVal = old
		connMu.Unlock()
	}
}

// LooksLikeSecret tells whether s has the shape of an agent token secret.
func LooksLikeSecret(s string) bool { return secretRe.MatchString(s) }

// resolve asks asc which token a secret is, as the host service.
func resolve(ctx context.Context, secret string) (*ascv1.AgentToken, error) {
	c, err := conn()
	if err != nil {
		if errors.Is(err, ErrDisabled) {
			return nil, ErrDisabled
		}
		return nil, ErrUnavailable
	}
	callCtx, cancel := context.WithTimeout(ascclient.WithSystem(ctx), ascclient.DefaultCallTimeout)
	defer cancel()
	resp, err := ascv1.NewAgentTokensClient(c).ResolveToken(callCtx, &ascv1.ResolveAgentTokenRequest{Secret: secret})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, ErrToken
		}
		return nil, ErrUnavailable
	}
	if resp.GetToken() == nil || resp.GetToken().GetId() == "" {
		return nil, ErrUnavailable
	}
	return resp.GetToken(), nil
}

// ascName is the name asc knows the repository by; ok is false for a repository asc does not keep.
func ascName(repo *repo_model.Repository) (string, bool) {
	if repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		return "", false
	}
	name, err := ascbackend.RepoName(setting.RepoRootPath, repo_model.RepoPath(repo.OwnerName, repo.Name))
	return name, err == nil
}

// contentOf finds the reported content in the repository.
func contentOf(ctx context.Context, repo *repo_model.Repository, req *Request) (moderation.ReportedContentType, int64, error) {
	switch req.Content {
	case ContentRepository:
		return moderation.ReportedContentTypeRepository, repo.ID, nil
	case ContentIssue:
		if req.Number <= 0 {
			return 0, 0, ErrInvalid
		}
		issue, err := issues.GetIssueByIndex(ctx, repo.ID, req.Number)
		if err != nil {
			if issues.IsErrIssueNotExist(err) {
				return 0, 0, ErrNotFound
			}
			return 0, 0, err
		}
		return moderation.ReportedContentTypeIssue, issue.ID, nil
	case ContentComment:
		if req.CommentID <= 0 {
			return 0, 0, ErrInvalid
		}
		comment, err := issues.GetCommentByID(ctx, req.CommentID)
		if err != nil {
			if issues.IsErrCommentNotExist(err) {
				return 0, 0, ErrNotFound
			}
			return 0, 0, err
		}
		if !comment.Type.HasContentSupport() {
			return 0, 0, ErrNotFound
		}
		if err := comment.LoadIssue(ctx); err != nil {
			if issues.IsErrIssueNotExist(err) {
				return 0, 0, ErrNotFound
			}
			return 0, 0, err
		}
		if comment.Issue.RepoID != repo.ID {
			return 0, 0, ErrNotFound // a comment of another repository looks like one that does not exist
		}
		return moderation.ReportedContentTypeComment, comment.ID, nil
	}
	return 0, 0, ErrInvalid
}

// agentName is the name a report shows for its agent: the agent of the token, else its name, cleaned.
func agentName(t *ascv1.AgentToken) string {
	name := t.GetAgent()
	if name == "" {
		name = t.GetName()
	}
	name = ascmoderation.Clean(name, maxAgentName)
	name = stripSpaces(name)
	for len(name) > maxAgentName { // runes were bounded; bytes must fit the column too
		r := []rune(name)
		name = string(r[:len(r)-1])
	}
	if name == "" {
		name = "agent"
	}
	return name
}

func stripSpaces(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if unicode.IsSpace(r) {
			r = ' '
		}
		out = append(out, r)
	}
	return string(out)
}

// Report checks the token and the content and stores the report. owner and name are the repository of the address,
// secret is the bearer the agent sent. The token is checked before anything about the repository is looked at, so a
// caller without a token for it cannot tell whether a private repository exists.
func Report(ctx context.Context, owner, name, secret string, req *Request) (*Result, error) {
	if !setting.Moderation.Enabled || !setting.Asc.Enabled {
		return nil, ErrDisabled
	}
	if !LooksLikeSecret(secret) {
		return nil, ErrToken
	}
	token, err := resolve(ctx, secret)
	if err != nil {
		return nil, err
	}
	if token.GetState() != ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE {
		return nil, ErrToken
	}
	repo, err := repo_model.GetRepositoryByOwnerAndName(ctx, owner, name)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return nil, ErrToken
		}
		return nil, err
	}
	if asc, ok := ascName(repo); !ok || token.GetRepo() != asc {
		return nil, ErrToken
	}
	category, ok := categories[req.Category]
	if !ok {
		return nil, ErrInvalid
	}
	remarks := ascmoderation.Clean(req.Remarks, MaxRemarks)
	if remarks == "" {
		return nil, ErrInvalid
	}
	contentType, contentID, err := contentOf(ctx, repo, req)
	if err != nil {
		return nil, err
	}
	report := &moderation.AbuseReport{
		ContentType:        contentType,
		ContentID:          contentID,
		Category:           category,
		Remarks:            remarks,
		ReporterAgent:      agentName(token),
		ReporterAgentToken: token.GetId(),
	}
	existing, err := moderation.ReportAbuseByAgent(ctx, report)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return &Result{ID: existing.ID, Duplicate: true}, nil
	}
	return &Result{ID: report.ID}, nil
}
