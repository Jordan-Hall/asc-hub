// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"context"
	"crypto/subtle"
	"encoding/base32"
	"strings"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
	"forgejo.org/modules/util"
)

// RecoveryCodeCount is how many recovery codes a person gets at a time.
const RecoveryCodeCount = 10

// TwoFactorRecoveryCode is one recovery code of a person: ten are made together, each opens the second factor once. Only a
// salted hash is kept; the code itself is shown once, when it is made.
type TwoFactorRecoveryCode struct {
	ID          int64              `xorm:"pk autoincr"`
	UID         int64              `xorm:"INDEX NOT NULL"`
	Salt        string             `xorm:"VARCHAR(64) NOT NULL"`
	Hash        string             `xorm:"VARCHAR(128) NOT NULL"`
	UsedUnix    timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
	CreatedUnix timeutil.TimeStamp `xorm:"created NOT NULL DEFAULT 0"`
}

func init() {
	db.RegisterModel(new(TwoFactorRecoveryCode))
}

// the letters are chosen to avoid the ones that look alike (0 O 1 I)
const recoveryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// normalizeRecoveryCode takes the spaces and dashes out of what was typed and upper-cases it.
func normalizeRecoveryCode(code string) string {
	code = strings.ToUpper(code)
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '	' {
			return -1
		}
		return r
	}, code)
}

// GenerateRecoveryCodes replaces the recovery codes of the person with RecoveryCodeCount new ones and returns them as they are
// shown, in groups of four letters ("ABCD-EFGH-JKLM"; 60 bits each). The old ones stop working.
func GenerateRecoveryCodes(ctx context.Context, uid int64) ([]string, error) {
	var shown []string
	err := db.WithTx(ctx, func(ctx context.Context) error {
		if _, err := db.GetEngine(ctx).Where("uid = ?", uid).Delete(new(TwoFactorRecoveryCode)); err != nil {
			return err
		}
		shown = shown[:0]
		for range RecoveryCodeCount {
			raw := base32.NewEncoding(recoveryAlphabet).WithPadding(base32.NoPadding).EncodeToString(util.CryptoRandomBytes(8))[:12]
			salt := util.CryptoRandomString(util.RandomStringMedium)
			row := &TwoFactorRecoveryCode{UID: uid, Salt: salt, Hash: HashToken(raw, salt)}
			if _, err := db.GetEngine(ctx).Insert(row); err != nil {
				return err
			}
			shown = append(shown, raw[0:4]+"-"+raw[4:8]+"-"+raw[8:12])
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return shown, nil
}

// UseRecoveryCode spends the code if it is one of the unused codes of the person. A code works once: two requests with the same
// code cannot both succeed.
func UseRecoveryCode(ctx context.Context, uid int64, code string) (bool, error) {
	code = normalizeRecoveryCode(code)
	if len(code) != 12 {
		return false, nil
	}
	var rows []*TwoFactorRecoveryCode
	if err := db.GetEngine(ctx).Where("uid = ? AND used_unix = 0", uid).Find(&rows); err != nil {
		return false, err
	}
	for _, r := range rows {
		if subtle.ConstantTimeCompare([]byte(r.Hash), []byte(HashToken(code, r.Salt))) != 1 {
			continue
		}
		n, err := db.GetEngine(ctx).Where("id = ? AND used_unix = 0", r.ID).Cols("used_unix").Update(&TwoFactorRecoveryCode{UsedUnix: timeutil.TimeStampNow()})
		if err != nil {
			return false, err
		}
		return n == 1, nil
	}
	return false, nil
}

// CountRecoveryCodes says how many of the person's codes are unused and how many were made.
func CountRecoveryCodes(ctx context.Context, uid int64) (unused, total int, err error) {
	t, err := db.GetEngine(ctx).Where("uid = ?", uid).Count(new(TwoFactorRecoveryCode))
	if err != nil {
		return 0, 0, err
	}
	u, err := db.GetEngine(ctx).Where("uid = ? AND used_unix = 0", uid).Count(new(TwoFactorRecoveryCode))
	if err != nil {
		return 0, 0, err
	}
	return int(u), int(t), nil
}

// DeleteRecoveryCodes removes the recovery codes of the person (the second factor is gone, so are they).
func DeleteRecoveryCodes(ctx context.Context, uid int64) error {
	_, err := db.GetEngine(ctx).Where("uid = ?", uid).Delete(new(TwoFactorRecoveryCode))
	return err
}
