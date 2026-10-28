// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascmaintenance is the maintenance mode of the hub: an administrator turns it on with a message and a window, and
// until it is off every change made through the browser answers 503 with that message, while reading goes on. The state
// is a site setting, so every process of the hub sees it within a few seconds.
package ascmaintenance

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	system_model "forgejo.org/models/system"
)

const (
	settingKey = "asc.maintenance"
	cacheFor   = 5 * time.Second
)

// State is what the administrator set.
type State struct {
	On        bool   `json:"on"`
	Message   string `json:"message,omitempty"`    // one or two sentences, shown on the page
	Window    string `json:"window,omitempty"`     // free text such as "22:00 to 23:00 UTC"
	Until     string `json:"until,omitempty"`      // expected back, RFC 3339; shown as a date
	StatusURL string `json:"status_url,omitempty"` // https address of a status page
}

// Validate refuses what the page cannot have sent.
func (s State) Validate() error {
	if len(s.Message) > 300 || len(s.Window) > 80 || len(s.StatusURL) > 300 {
		return errors.New("a text is too long")
	}
	if strings.ContainsAny(s.Message+s.Window, "\r\n\x00") {
		return errors.New("a text has a line break")
	}
	if s.Until != "" {
		if _, err := time.Parse(time.RFC3339, s.Until); err != nil {
			return errors.New("expected back must be a date and time")
		}
	}
	if s.StatusURL != "" {
		u, err := url.Parse(s.StatusURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return errors.New("the status address must be an https address")
		}
	}
	return nil
}

// UntilTime returns the expected time back, if there is one.
func (s State) UntilTime() (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s.Until)
	return t, err == nil
}

var (
	mu       sync.Mutex
	cached   State
	cachedAt time.Time
)

// Load returns the state; it asks the database at most every few seconds.
func Load(ctx context.Context) State {
	mu.Lock()
	defer mu.Unlock()
	if !cachedAt.IsZero() && time.Since(cachedAt) < cacheFor {
		return cached
	}
	cachedAt = time.Now()
	_, all, err := system_model.GetAllSettings(ctx)
	if err != nil {
		return cached // keep the last known state
	}
	var s State
	if raw := all[settingKey]; raw == "" || json.Unmarshal([]byte(raw), &s) != nil {
		s = State{}
	}
	cached = s
	return cached
}

// Save validates and stores the state, and makes this process see it at once.
func Save(ctx context.Context, s State) error {
	if err := s.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := system_model.SetSettings(ctx, map[string]string{settingKey: string(raw)}); err != nil {
		return err
	}
	mu.Lock()
	cached, cachedAt = s, time.Now()
	mu.Unlock()
	return nil
}

// Forget drops the cache; tests use it.
func Forget() {
	mu.Lock()
	cached, cachedAt = State{}, time.Time{}
	mu.Unlock()
}
