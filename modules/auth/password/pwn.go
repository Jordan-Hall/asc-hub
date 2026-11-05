// Copyright 2020 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package password

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"forgejo.org/modules/auth/password/pwn"
	"forgejo.org/modules/setting"
)

var ErrIsPwned = errors.New("password has been pwned")

type ErrIsPwnedRequest struct {
	err error
}

func IsErrIsPwnedRequest(err error) bool {
	_, ok := err.(ErrIsPwnedRequest)
	return ok
}

func (err ErrIsPwnedRequest) Error() string {
	return fmt.Sprintf("using Have-I-Been-Pwned service failed: %v", err.err)
}

func (err ErrIsPwnedRequest) Unwrap() error {
	return err.err
}

// PwnHTTPClient is the client that asks the Have-I-Been-Pwned range API; tests replace it.
var PwnHTTPClient = http.DefaultClient

// PwnedRange returns the suffixes of the breached password hashes that start with prefix (five hexadecimal digits of
// a SHA-1), for the live breach rule of the strength meter. A failure of the service is ErrIsPwnedRequest.
func PwnedRange(ctx context.Context, prefix string) ([]string, error) {
	suffixes, err := pwn.New(pwn.WithContext(ctx), pwn.WithHTTP(PwnHTTPClient)).Range(prefix)
	if err != nil && !errors.Is(err, pwn.ErrBadPrefix) {
		return nil, ErrIsPwnedRequest{err}
	}
	return suffixes, err
}

// IsPwned checks whether a password has been pwned
// If a password has not been pwned, no error is returned.
func IsPwned(ctx context.Context, password string) error {
	if !setting.PasswordCheckPwn {
		return nil
	}

	client := pwn.New(pwn.WithContext(ctx), pwn.WithHTTP(PwnHTTPClient))
	count, err := client.CheckPassword(password, true)
	if err != nil {
		return ErrIsPwnedRequest{err}
	}

	if count > 0 {
		return ErrIsPwned
	}

	return nil
}
