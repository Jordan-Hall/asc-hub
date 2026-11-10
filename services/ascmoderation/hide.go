// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascmoderation

import (
	"context"
	"errors"
	"fmt"

	"forgejo.org/models/db"
	"forgejo.org/models/issues"
	"forgejo.org/models/moderation"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	issue_indexer "forgejo.org/modules/indexer/issues"
	"forgejo.org/modules/json"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/structs"
	repo_service "forgejo.org/services/repository"
	user_service "forgejo.org/services/user"
)

// What hidden content shows in place of its title and text, to everyone, the owner included: the owner reads why on
// the page of the decision.
const (
	HiddenTitle = "Hidden by a moderator"
	HiddenText  = "_Hidden by a moderator._"
)

// ErrAlreadyHidden is returned when the content is hidden already.
var ErrAlreadyHidden = errors.New("ascmoderation: the content is hidden already")

// saved is what hiding changed, kept to undo it.
type saved struct {
	Visibility *structs.VisibleType `json:"visibility,omitempty"`
	Private    *bool                `json:"private,omitempty"`
	Internal   *bool                `json:"internal,omitempty"`
	Title      *string              `json:"title,omitempty"`
	Content    *string              `json:"content,omitempty"`
}

// Hide takes reported content out of public view and keeps what it changed: a profile becomes private, a repository
// private, an issue's title and text and a comment's text are replaced with HiddenTitle and HiddenText. The content is
// not deleted; Unhide puts it back. Content already hidden gives ErrAlreadyHidden.
func Hide(ctx context.Context, moderator *user_model.User, contentType moderation.ReportedContentType, contentID int64) error {
	if hidden, err := moderation.IsHidden(ctx, contentType, contentID); err != nil {
		return err
	} else if hidden {
		return ErrAlreadyHidden
	}
	var s saved
	var reindex int64
	err := db.WithTx(ctx, func(ctx context.Context) error {
		switch contentType {
		case moderation.ReportedContentTypeUser:
			u, err := user_model.GetUserByID(ctx, contentID)
			if err != nil {
				return err
			}
			v := u.Visibility
			s.Visibility = &v
			if err := user_service.UpdateUser(ctx, u, &user_service.UpdateOptions{Visibility: optional.Some(structs.VisibleTypePrivate)}); err != nil {
				return err
			}
		case moderation.ReportedContentTypeRepository:
			repo, err := repo_model.GetRepositoryByID(ctx, contentID)
			if err != nil {
				return err
			}
			p, in := repo.IsPrivate, repo.IsInternal
			s.Private, s.Internal = &p, &in
			if !repo.IsPrivate || repo.IsInternal {
				repo.IsPrivate, repo.IsInternal = true, false
				if err := repo_service.UpdateRepository(ctx, repo, true); err != nil {
					return err
				}
			}
		case moderation.ReportedContentTypeIssue:
			issue, err := issues.GetIssueByID(ctx, contentID)
			if err != nil {
				return err
			}
			title, content := issue.Title, issue.Content
			s.Title, s.Content = &title, &content
			issue.Title, issue.Content = HiddenTitle, HiddenText
			if err := issues.UpdateIssueCols(ctx, issue, "name", "content"); err != nil {
				return err
			}
			reindex = issue.ID
		case moderation.ReportedContentTypeComment:
			c, err := issues.GetCommentByID(ctx, contentID)
			if err != nil {
				return err
			}
			content := c.Content
			s.Content = &content
			c.Content = HiddenText
			if _, err := db.GetEngine(ctx).ID(c.ID).Cols("content").NoAutoTime().Update(c); err != nil {
				return err
			}
			reindex = c.IssueID
		default:
			return fmt.Errorf("ascmoderation: unknown content type %d", contentType)
		}
		raw, err := json.Marshal(s)
		if err != nil {
			return err
		}
		return moderation.InsertHidden(ctx, &moderation.Hidden{ContentType: contentType, ContentID: contentID, Saved: string(raw), ModeratorID: moderator.ID})
	})
	if err == nil && reindex > 0 {
		issue_indexer.UpdateIssueIndexer(ctx, reindex)
	}
	return err
}

// Unhide puts hidden content back as it was before Hide. Content that is not hidden, or that is gone, is left alone.
func Unhide(ctx context.Context, contentType moderation.ReportedContentType, contentID int64) error {
	h, err := moderation.GetHidden(ctx, contentType, contentID)
	if err != nil || h == nil {
		return err
	}
	var s saved
	if err := json.Unmarshal([]byte(h.Saved), &s); err != nil {
		return err
	}
	var reindex int64
	err = db.WithTx(ctx, func(ctx context.Context) error {
		switch contentType {
		case moderation.ReportedContentTypeUser:
			u, err := user_model.GetUserByID(ctx, contentID)
			if user_model.IsErrUserNotExist(err) {
				break
			} else if err != nil {
				return err
			}
			if s.Visibility != nil {
				if err := user_service.UpdateUser(ctx, u, &user_service.UpdateOptions{Visibility: optional.Some(*s.Visibility)}); err != nil {
					return err
				}
			}
		case moderation.ReportedContentTypeRepository:
			repo, err := repo_model.GetRepositoryByID(ctx, contentID)
			if repo_model.IsErrRepoNotExist(err) {
				break
			} else if err != nil {
				return err
			}
			internal := s.Internal != nil && *s.Internal
			if s.Private != nil && (repo.IsPrivate != *s.Private || repo.IsInternal != internal) {
				repo.IsPrivate, repo.IsInternal = *s.Private, internal
				if err := repo_service.UpdateRepository(ctx, repo, true); err != nil {
					return err
				}
			}
		case moderation.ReportedContentTypeIssue:
			issue, err := issues.GetIssueByID(ctx, contentID)
			if issues.IsErrIssueNotExist(err) {
				break
			} else if err != nil {
				return err
			}
			if s.Title != nil && s.Content != nil {
				issue.Title, issue.Content = *s.Title, *s.Content
				if err := issues.UpdateIssueCols(ctx, issue, "name", "content"); err != nil {
					return err
				}
				reindex = issue.ID
			}
		case moderation.ReportedContentTypeComment:
			c, err := issues.GetCommentByID(ctx, contentID)
			if issues.IsErrCommentNotExist(err) {
				break
			} else if err != nil {
				return err
			}
			if s.Content != nil {
				c.Content = *s.Content
				if _, err := db.GetEngine(ctx).ID(c.ID).Cols("content").NoAutoTime().Update(c); err != nil {
					return err
				}
				reindex = c.IssueID
			}
		}
		return moderation.DeleteHidden(ctx, contentType, contentID)
	})
	if err == nil && reindex > 0 {
		issue_indexer.UpdateIssueIndexer(ctx, reindex)
	}
	return err
}
