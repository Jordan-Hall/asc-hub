// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
)

// The device authorisation grant (RFC 8628) of the asc command line: `asc auth login --host <hub>` asks for a
// device code, shows the person a short user code, and polls until the person approves it in a browser session.
// The approval creates an ordinary personal access token, so there is no second token system.

const (
	// AscDeviceCodeLifetime is how long a device code can be approved.
	AscDeviceCodeLifetime = 10 * time.Minute
	// AscDeviceCodeInterval is the least time between two polls of one device code.
	AscDeviceCodeInterval = 5 * time.Second

	userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ" // no vowels, so a code never spells a word
	userCodeLength   = 8
)

// AscDeviceCodeStatus is where a device code is in its life.
type AscDeviceCodeStatus int

const (
	AscDeviceCodePending  AscDeviceCodeStatus = iota // asked for, not yet decided
	AscDeviceCodeApproved                            // approved by UserID, token not handed out yet
	AscDeviceCodeDenied                              // refused by UserID
	AscDeviceCodeConsumed                            // approved and the token was handed out
)

// AscDeviceCode is one pending or decided device authorisation. Only the SHA-256 of the device code is stored: the
// device code is the secret of the terminal, the user code is what a person types and is stored in its normal form.
type AscDeviceCode struct {
	ID             int64               `xorm:"pk autoincr"`
	DeviceCodeHash string              `xorm:"VARCHAR(64) UNIQUE NOT NULL"`
	UserCode       string              `xorm:"VARCHAR(16) UNIQUE NOT NULL"` // normal form: capitals, no dash
	DeviceName     string              `xorm:"VARCHAR(128) NOT NULL DEFAULT ''"`
	Status         AscDeviceCodeStatus `xorm:"NOT NULL DEFAULT 0"`
	UserID         int64               `xorm:"NOT NULL DEFAULT 0"`
	ExpiresUnix    timeutil.TimeStamp  `xorm:"INDEX NOT NULL"`
	LastPollUnix   timeutil.TimeStamp  `xorm:"NOT NULL DEFAULT 0"`
	CreatedUnix    timeutil.TimeStamp  `xorm:"created"`
}

func init() {
	db.RegisterModel(new(AscDeviceCode))
}

// TableName is the table of the model.
func (AscDeviceCode) TableName() string { return "asc_device_code" }

// Errors of the device flow. They map to the error codes of RFC 8628 section 3.5.
var (
	ErrAscDeviceCodeNotFound  = errors.New("device code not found")
	ErrAscDeviceCodeExpired   = errors.New("device code expired")
	ErrAscDeviceCodeDecided   = errors.New("device code already decided")
	ErrAscDeviceCodeDenied    = errors.New("device code denied")
	ErrAscDeviceCodePending   = errors.New("authorization pending")
	ErrAscDeviceCodeSlowDown  = errors.New("polling too fast")
	ErrAscDeviceCodeBadDevice = errors.New("device name is not valid")
)

// HashAscDeviceCode is the stored form of a device code.
func HashAscDeviceCode(deviceCode string) string {
	sum := sha256.Sum256([]byte(deviceCode))
	return hex.EncodeToString(sum[:])
}

// NormalizeAscUserCode turns what a person typed (any case, dashes and spaces) into the stored form.
func NormalizeAscUserCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if strings.ContainsRune(userCodeAlphabet, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FormatAscUserCode writes a stored user code the way it is shown: WDJB-MJHT.
func FormatAscUserCode(code string) string {
	if len(code) != userCodeLength {
		return code
	}
	return code[:4] + "-" + code[4:]
}

func newUserCode() (string, error) {
	out := make([]byte, 0, userCodeLength)
	var one [1]byte
	for len(out) < userCodeLength {
		if _, err := rand.Read(one[:]); err != nil {
			return "", err
		}
		// 240 is the largest multiple of 20 below 256: a byte above it would make some letters likelier
		if one[0] >= 240 {
			continue
		}
		out = append(out, userCodeAlphabet[int(one[0])%len(userCodeAlphabet)])
	}
	return string(out), nil
}

// CleanAscDeviceName keeps the name the terminal gave itself to one short line of printable text.
func CleanAscDeviceName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > 100 {
		return "", ErrAscDeviceCodeBadDevice
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", ErrAscDeviceCodeBadDevice
		}
	}
	return s, nil
}

// CreateAscDeviceCode starts a device authorisation. It returns the row and the device code, which is shown once,
// to the terminal.
func CreateAscDeviceCode(ctx context.Context, deviceName string) (row *AscDeviceCode, deviceCode string, err error) {
	if deviceName, err = CleanAscDeviceName(deviceName); err != nil {
		return nil, "", err
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return nil, "", err
	}
	deviceCode = hex.EncodeToString(raw)
	for range 5 {
		code, cerr := newUserCode()
		if cerr != nil {
			return nil, "", cerr
		}
		row = &AscDeviceCode{
			DeviceCodeHash: HashAscDeviceCode(deviceCode),
			UserCode:       code,
			DeviceName:     deviceName,
			ExpiresUnix:    timeutil.TimeStampNow().Add(int64(AscDeviceCodeLifetime / time.Second)),
		}
		if _, err = db.GetEngine(ctx).Insert(row); err == nil {
			return row, deviceCode, nil
		}
		// the user code collided with a stored one: try another
		if exist, _ := db.GetEngine(ctx).Where("user_code = ?", code).Exist(new(AscDeviceCode)); !exist {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("no free user code: %w", err)
}

// GetAscDeviceCodeByUserCode finds a device code by what a person typed. An expired code is reported as expired.
func GetAscDeviceCodeByUserCode(ctx context.Context, typed string) (*AscDeviceCode, error) {
	code := NormalizeAscUserCode(typed)
	if len(code) != userCodeLength {
		return nil, ErrAscDeviceCodeNotFound
	}
	row := new(AscDeviceCode)
	has, err := db.GetEngine(ctx).Where("user_code = ?", code).Get(row)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrAscDeviceCodeNotFound
	}
	if row.Expired() {
		return nil, ErrAscDeviceCodeExpired
	}
	return row, nil
}

// Expired reports whether the code can no longer be approved or exchanged.
func (c *AscDeviceCode) Expired() bool {
	return timeutil.TimeStampNow() >= c.ExpiresUnix
}

// Decide records the answer of a person. It succeeds once: a code that is decided stays as it was.
func (c *AscDeviceCode) Decide(ctx context.Context, userID int64, approve bool) error {
	if c.Expired() {
		return ErrAscDeviceCodeExpired
	}
	status := AscDeviceCodeDenied
	if approve {
		status = AscDeviceCodeApproved
	}
	n, err := db.GetEngine(ctx).Where("id = ? AND status = ? AND expires_unix > ?", c.ID, AscDeviceCodePending, timeutil.TimeStampNow()).
		Cols("status", "user_id").Update(&AscDeviceCode{Status: status, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		// nothing changed: the code ran out between loading it and answering, or somebody answered first
		{
			fresh := new(AscDeviceCode)
			if has, err := db.GetEngine(ctx).ID(c.ID).Get(fresh); err == nil && has && fresh.Expired() {
				return ErrAscDeviceCodeExpired
			}
		}
		return ErrAscDeviceCodeDecided
	}
	c.Status, c.UserID = status, userID
	return nil
}

// ExchangeAscDeviceCode is the poll of the terminal. While the person has not answered it returns
// ErrAscDeviceCodePending (ErrAscDeviceCodeSlowDown when the terminal polls faster than the interval); after a
// denial ErrAscDeviceCodeDenied; after the lifetime ErrAscDeviceCodeExpired. After an approval it creates the
// personal access token once and returns it; a second exchange finds the code consumed.
func ExchangeAscDeviceCode(ctx context.Context, deviceCode string, scope AccessTokenScope) (*AccessToken, error) {
	row := new(AscDeviceCode)
	has, err := db.GetEngine(ctx).Where("device_code_hash = ?", HashAscDeviceCode(deviceCode)).Get(row)
	if err != nil {
		return nil, err
	}
	if !has || row.Status == AscDeviceCodeConsumed {
		return nil, ErrAscDeviceCodeNotFound
	}
	if row.Expired() {
		return nil, ErrAscDeviceCodeExpired
	}
	switch row.Status {
	case AscDeviceCodePending:
		now := timeutil.TimeStampNow()
		slow := row.LastPollUnix != 0 && int64(now-row.LastPollUnix) < int64(AscDeviceCodeInterval/time.Second)
		if _, err := db.GetEngine(ctx).ID(row.ID).Cols("last_poll_unix").Update(&AscDeviceCode{LastPollUnix: now}); err != nil {
			return nil, err
		}
		if slow {
			return nil, ErrAscDeviceCodeSlowDown
		}
		return nil, ErrAscDeviceCodePending
	case AscDeviceCodeDenied:
		return nil, ErrAscDeviceCodeDenied
	}
	// approved: hand the token out exactly once, in one transaction with marking the code consumed
	var token *AccessToken
	err = db.WithTx(ctx, func(ctx context.Context) error {
		n, err := db.GetEngine(ctx).Where("id = ? AND status = ?", row.ID, AscDeviceCodeApproved).
			Cols("status").Update(&AscDeviceCode{Status: AscDeviceCodeConsumed})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrAscDeviceCodeNotFound
		}
		name := "asc"
		if row.DeviceName != "" {
			name = "asc on " + row.DeviceName
		}
		token = &AccessToken{UID: row.UserID, Name: name + " (" + FormatAscUserCode(row.UserCode) + ")", Scope: scope}
		return NewAccessToken(ctx, token)
	})
	if err != nil {
		return nil, err
	}
	return token, nil
}

// DeleteExpiredAscDeviceCodes removes codes that ended a day ago, decided or not.
func DeleteExpiredAscDeviceCodes(ctx context.Context) (int64, error) {
	return db.GetEngine(ctx).Where("expires_unix < ?", timeutil.TimeStampNow().Add(-86400)).Delete(new(AscDeviceCode))
}
