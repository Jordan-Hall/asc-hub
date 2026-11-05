// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package pwn

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"forgejo.org/modules/setting"
)

const passwordURL = "https://api.pwnedpasswords.com/range/"

// ErrEmptyPassword is an empty password error
var ErrEmptyPassword = errors.New("password cannot be empty")

// Client is a HaveIBeenPwned client
type Client struct {
	ctx  context.Context
	http *http.Client
}

// New returns a new HaveIBeenPwned Client
func New(options ...ClientOption) *Client {
	client := &Client{
		ctx:  context.Background(),
		http: http.DefaultClient,
	}

	for _, opt := range options {
		opt(client)
	}

	return client
}

// ClientOption is a way to modify a new Client
type ClientOption func(*Client)

// WithHTTP will set the http.Client of a Client
func WithHTTP(httpClient *http.Client) func(pwnClient *Client) {
	return func(pwnClient *Client) {
		pwnClient.http = httpClient
	}
}

// WithContext will set the context.Context of a Client
func WithContext(ctx context.Context) func(pwnClient *Client) {
	return func(pwnClient *Client) {
		pwnClient.ctx = ctx
	}
}

func newRequest(ctx context.Context, method, url string, body io.ReadCloser) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Add("User-Agent", "Gitea "+setting.AppVer)
	return req, nil
}

// CheckPassword returns the number of times a password has been compromised
// Adding padding will make requests more secure, however is also slower
// because artificial responses will be added to the response
// For more information, see https://www.troyhunt.com/enhancing-pwned-passwords-privacy-with-padding/
func (c *Client) CheckPassword(pw string, padding bool) (int, error) {
	if pw == "" {
		return -1, ErrEmptyPassword
	}

	sha := sha1.New()
	sha.Write([]byte(pw))
	enc := hex.EncodeToString(sha.Sum(nil))
	prefix, suffix := enc[:5], enc[5:]

	body, err := c.fetchRange(prefix, padding)
	if err != nil {
		return -1, err
	}

	for pair := range strings.SplitSeq(body, "\n") {
		parts := strings.Split(pair, ":")
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(suffix, parts[0]) {
			count, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
			if err != nil {
				return -1, err
			}
			return int(count), nil
		}
	}
	return 0, nil
}

// ErrBadPrefix is returned by Range for a prefix that is not five hexadecimal digits.
var ErrBadPrefix = errors.New("the prefix must be five hexadecimal digits")

// Range returns the suffixes (35 upper case hexadecimal digits) of the breached passwords whose SHA-1 starts with
// prefix, so a caller can check a password without sending it: only the first five digits of its hash leave. The
// padding entries (count 0) are left out.
func (c *Client) Range(prefix string) ([]string, error) {
	if len(prefix) != 5 {
		return nil, ErrBadPrefix
	}
	if _, err := hex.DecodeString(prefix + "0"); err != nil {
		return nil, ErrBadPrefix
	}
	body, err := c.fetchRange(strings.ToUpper(prefix), true)
	if err != nil {
		return nil, err
	}
	var out []string
	for pair := range strings.SplitSeq(body, "\n") {
		suffix, count, ok := strings.Cut(strings.TrimSpace(pair), ":")
		if !ok || len(suffix) != 35 {
			continue
		}
		if n, err := strconv.ParseInt(count, 10, 64); err != nil || n <= 0 {
			continue
		}
		out = append(out, strings.ToUpper(suffix))
	}
	return out, nil
}

// fetchRange asks the range API for prefix.
func (c *Client) fetchRange(prefix string, padding bool) (string, error) {
	req, err := newRequest(c.ctx, http.MethodGet, fmt.Sprintf("%s%s", passwordURL, prefix), nil)
	if err != nil {
		return "", err
	}
	if padding {
		req.Header.Add("Add-Padding", "true")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the range API answered %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}
