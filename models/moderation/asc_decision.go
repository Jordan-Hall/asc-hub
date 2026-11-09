// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package moderation

import (
	"context"
	"errors"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"

	"xorm.io/builder"
)

// AppealStatus is where the appeal of a decision stands.
type AppealStatus int

const (
	// AppealNone: the owner has not appealed.
	AppealNone AppealStatus = iota
	// AppealOpen: the owner appealed and no administrator has answered.
	AppealOpen
	// AppealUpheld: an administrator kept the decision.
	AppealUpheld
	// AppealOverturned: an administrator reversed the decision (a suspension is lifted; deleted content is not restored).
	AppealOverturned
)

// MaxAppealText bounds what an owner writes in an appeal.
const MaxAppealText = 1000

// ErrAppealNotAllowed is returned when the decision is not the person's, or was already appealed or answered.
var ErrAppealNotAllowed = errors.New("moderation: this decision cannot be appealed now")

// Decision is one action an administrator took on reported content: what it was, whose it was, why, and the appeal of
// its owner. Reports are resolved and removed after a while (RemoveResolvedReports); a decision stays, so an owner can
// see why and appeal.
type Decision struct {
	ID          int64               `xorm:"pk autoincr"`
	ContentType ReportedContentType `xorm:"NOT NULL"`
	ContentID   int64               `xorm:"NOT NULL"`
	// ContentRef is what the content was called when the action was taken (@name, owner/repo, owner/repo#3).
	ContentRef string `xorm:"VARCHAR(255)"`
	// OwnerID is the account the content belonged to: the account itself, the owner of the repository, the poster of
	// the issue or comment. Only that account may appeal.
	OwnerID int64 `xorm:"INDEX NOT NULL"`
	// Action is the content action of services/moderation (suspend, delete account, repository, issue, comment).
	Action      int                `xorm:"NOT NULL"`
	Reason      string             `xorm:"VARCHAR(500)"`
	ModeratorID int64              `xorm:"NOT NULL"`
	CreatedUnix timeutil.TimeStamp `xorm:"created INDEX NOT NULL"`

	AppealStatus      AppealStatus       `xorm:"INDEX NOT NULL DEFAULT 0"`
	AppealText        string             `xorm:"VARCHAR(1000)"`
	AppealUnix        timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
	AppealDeciderID   int64              `xorm:"NOT NULL DEFAULT 0"`
	AppealReason      string             `xorm:"VARCHAR(500)"`
	AppealDecidedUnix timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
}

// TableName is the table of decisions.
func (Decision) TableName() string { return "asc_moderation_decision" }

func init() {
	db.RegisterModel(new(Decision))
}

// IsAppealOpen tells whether the owner's appeal waits for an answer.
func (d *Decision) IsAppealOpen() bool { return d.AppealStatus == AppealOpen }

// CanAppeal tells whether the owner may still appeal.
func (d *Decision) CanAppeal() bool { return d.AppealStatus == AppealNone }

// RecordDecision stores an action that was taken.
func RecordDecision(ctx context.Context, d *Decision) error {
	d.ID = 0
	d.AppealStatus = AppealNone
	_, err := db.GetEngine(ctx).Insert(d)
	return err
}

// GetDecision returns one decision, nil when there is none.
func GetDecision(ctx context.Context, id int64) (*Decision, error) {
	d := new(Decision)
	has, err := db.GetEngine(ctx).ID(id).Get(d)
	if err != nil || !has {
		return nil, err
	}
	return d, nil
}

// DecisionsOf returns the decisions about the content of one account, the newest first, at most limit.
func DecisionsOf(ctx context.Context, ownerID int64, limit int) ([]*Decision, error) {
	out := make([]*Decision, 0, 8)
	err := db.GetEngine(ctx).Where("owner_id = ?", ownerID).Desc("created_unix", "id").Limit(limit).Find(&out)
	return out, err
}

// HasDecisions tells whether any decision is about the content of one account.
func HasDecisions(ctx context.Context, ownerID int64) (bool, error) {
	return db.GetEngine(ctx).Where("owner_id = ?", ownerID).Exist(new(Decision))
}

// LatestSuspension returns the newest suspension of an account, nil when there is none.
func LatestSuspension(ctx context.Context, ownerID int64, suspendAction int) (*Decision, error) {
	d := new(Decision)
	has, err := db.GetEngine(ctx).
		Where(builder.Eq{"owner_id": ownerID, "action": suspendAction, "content_type": ReportedContentTypeUser}).
		Desc("created_unix", "id").Get(d)
	if err != nil || !has {
		return nil, err
	}
	return d, nil
}

// Appeal records the owner's appeal of a decision. Only the owner may appeal, and only once.
func Appeal(ctx context.Context, id, ownerID int64, text string) error {
	if text == "" || len([]rune(text)) > MaxAppealText {
		return ErrAppealNotAllowed
	}
	n, err := db.GetEngine(ctx).
		Where(builder.Eq{"id": id, "owner_id": ownerID, "appeal_status": AppealNone}).
		Cols("appeal_status", "appeal_text", "appeal_unix").
		Update(&Decision{AppealStatus: AppealOpen, AppealText: text, AppealUnix: timeutil.TimeStampNow()})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAppealNotAllowed
	}
	return nil
}

// OpenAppeals returns the appeals that wait for an answer, the oldest first, at most limit.
func OpenAppeals(ctx context.Context, limit int) ([]*Decision, error) {
	out := make([]*Decision, 0, 8)
	err := db.GetEngine(ctx).Where("appeal_status = ?", AppealOpen).Asc("appeal_unix", "id").Limit(limit).Find(&out)
	return out, err
}

// CountOpenAppeals counts the appeals that wait for an answer.
func CountOpenAppeals(ctx context.Context) (int64, error) {
	return db.GetEngine(ctx).Where("appeal_status = ?", AppealOpen).Count(new(Decision))
}

// DecideAppeal answers an open appeal: upheld or overturned, with the administrator's reason. It returns
// ErrAppealNotAllowed when the appeal is not open (answered already, or never made).
func DecideAppeal(ctx context.Context, id, deciderID int64, overturn bool, reason string) error {
	status := AppealUpheld
	if overturn {
		status = AppealOverturned
	}
	n, err := db.GetEngine(ctx).
		Where(builder.Eq{"id": id, "appeal_status": AppealOpen}).
		Cols("appeal_status", "appeal_decider_id", "appeal_reason", "appeal_decided_unix").
		Update(&Decision{AppealStatus: status, AppealDeciderID: deciderID, AppealReason: reason, AppealDecidedUnix: timeutil.TimeStampNow()})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAppealNotAllowed
	}
	return nil
}
