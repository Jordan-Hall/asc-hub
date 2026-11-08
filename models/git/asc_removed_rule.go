// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package git

import (
	"context"
	"errors"
	"fmt"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/json"
	"forgejo.org/modules/timeutil"
)

// AscRuleUndoWindow is how long a removed branch rule can be put back (board RepoSettingsRules).
const AscRuleUndoWindow = 30 * time.Minute

// ErrAscRuleUndoGone is returned for a removal that is unknown, of another repository or past AscRuleUndoWindow.
var ErrAscRuleUndoGone = errors.New("the removed rule can no longer be put back")

// ErrAscRuleUndoTaken is returned when a rule of the same name was added after the removal.
type ErrAscRuleUndoTaken struct{ Name string }

func (e ErrAscRuleUndoTaken) Error() string {
	return fmt.Sprintf("a rule named %q exists again", e.Name)
}

// ErrAscRuleUndoOverlap is returned when a rule added after the removal protects a branch the removed one did.
type ErrAscRuleUndoOverlap struct{ Name, With string }

func (e ErrAscRuleUndoOverlap) Error() string {
	return fmt.Sprintf("the rule %q now overlaps the rule %q", e.Name, e.With)
}

// AscRemovedRule keeps a removed branch rule for AscRuleUndoWindow, so the rules page can offer to put it back.
type AscRemovedRule struct {
	ID          int64              `xorm:"pk autoincr"`
	RepoID      int64              `xorm:"INDEX NOT NULL"`
	RuleName    string             `xorm:"NOT NULL"`
	Rule        string             `xorm:"LONGTEXT NOT NULL"` // the removed ProtectedBranch as JSON
	RemovedBy   int64              `xorm:"NOT NULL DEFAULT 0"`
	RemovedUnix timeutil.TimeStamp `xorm:"INDEX NOT NULL"`
}

func init() {
	db.RegisterModel(new(AscRemovedRule))
}

// TableName is the table of the model.
func (AscRemovedRule) TableName() string { return "asc_removed_rule" }

// Until is when the removal can no longer be undone.
func (r *AscRemovedRule) Until() timeutil.TimeStamp {
	return r.RemovedUnix.AddDuration(AscRuleUndoWindow)
}

// ascRuleUndoCutoff is the oldest removal time that can still be undone.
func ascRuleUndoCutoff() timeutil.TimeStamp {
	return timeutil.TimeStampNow().AddDuration(-AscRuleUndoWindow)
}

// AscRemoveRule removes the rule and keeps a copy of it for AscRuleUndoWindow. Copies past the window are dropped.
func AscRemoveRule(ctx context.Context, repo *repo_model.Repository, rule *ProtectedBranch, doerID int64) error {
	kept := *rule
	kept.Repo = nil
	data, err := json.Marshal(&kept)
	if err != nil {
		return err
	}
	return db.WithTx(ctx, func(ctx context.Context) error {
		if err := DeleteProtectedBranch(ctx, repo, rule.ID); err != nil {
			return err
		}
		e := db.GetEngine(ctx)
		if _, err := e.Where("repo_id = ? AND removed_unix < ?", repo.ID, ascRuleUndoCutoff()).
			Delete(new(AscRemovedRule)); err != nil {
			return err
		}
		_, err := e.Insert(&AscRemovedRule{
			RepoID:      repo.ID,
			RuleName:    rule.RuleName,
			Rule:        string(data),
			RemovedBy:   doerID,
			RemovedUnix: timeutil.TimeStampNow(),
		})
		return err
	})
}

// AscRemovedRules returns the removals of the repository that can still be undone, newest first.
func AscRemovedRules(ctx context.Context, repoID int64) ([]*AscRemovedRule, error) {
	var rows []*AscRemovedRule
	err := db.GetEngine(ctx).Where("repo_id = ? AND removed_unix >= ?", repoID, ascRuleUndoCutoff()).
		Desc("removed_unix", "id").Find(&rows)
	return rows, err
}

// AscRestoreRule puts the removed rule back as it was and forgets the removal. It refuses a removal past the
// window (ErrAscRuleUndoGone), a name in use again (ErrAscRuleUndoTaken) and an overlap with a rule added since
// (ErrAscRuleUndoOverlap).
func AscRestoreRule(ctx context.Context, repo *repo_model.Repository, removedID int64) (*ProtectedBranch, error) {
	if err := repo.MustNotBeArchived(); err != nil {
		return nil, err
	}
	var rule ProtectedBranch
	err := db.WithTx(ctx, func(ctx context.Context) error {
		row := &AscRemovedRule{}
		has, err := db.GetEngine(ctx).Where("id = ? AND repo_id = ?", removedID, repo.ID).Get(row)
		if err != nil {
			return err
		}
		if !has || row.RemovedUnix < ascRuleUndoCutoff() {
			return ErrAscRuleUndoGone
		}
		if err := json.Unmarshal([]byte(row.Rule), &rule); err != nil {
			return err
		}
		if same, err := GetProtectedBranchRuleByName(ctx, repo.ID, rule.RuleName); err != nil {
			return err
		} else if same != nil {
			return ErrAscRuleUndoTaken{Name: rule.RuleName}
		}
		if other, err := AscOverlappingRule(ctx, repo.ID, 0, rule.RuleName); err != nil {
			return err
		} else if other != nil {
			return ErrAscRuleUndoOverlap{Name: rule.RuleName, With: other.RuleName}
		}
		rule.ID, rule.RepoID = 0, repo.ID
		if _, err := db.GetEngine(ctx).Insert(&rule); err != nil {
			return err
		}
		_, err = db.GetEngine(ctx).ID(row.ID).Delete(new(AscRemovedRule))
		return err
	})
	if err != nil {
		return nil, err
	}
	return &rule, nil
}
