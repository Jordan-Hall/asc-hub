// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascpush sends the notifications of the hub to the browsers a person subscribed (Web Push). The hub signs
// with its own VAPID key pair, made on first use and kept encrypted in the settings of the site. It asks ascnotify
// whether the person wants the event now, and sends only the title, a few words and a path of the hub.
package ascpush

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	system_model "forgejo.org/models/system"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/hostmatcher"
	"forgejo.org/modules/log"
	"forgejo.org/modules/secret"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/webpush"
	"forgejo.org/services/ascnotify"
)

const (
	vapidSettingKey = "asc.vapid_private"
	sendTimeout     = 10 * time.Second
	maxInFlight     = 8
	maxTextBytes    = 300
)

var (
	keysMu sync.Mutex
	keys   *webpush.Keys

	clientOnce sync.Once
	client     *http.Client
	slots      = make(chan struct{}, maxInFlight)
)

// httpClient never connects to an address of this machine or network, whatever the name resolves to (the endpoint is
// a text a person sent), and never follows a redirect.
func httpClient() *http.Client {
	clientOnce.Do(func() {
		matcher := hostmatcher.ParseHostMatchList("webpush", hostmatcher.MatchBuiltinExternal)
		client = &http.Client{
			Timeout:       sendTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
				DialContext:     hostmatcher.NewDialContext("webpush", matcher, nil, nil),
			},
		}
	})
	return client
}

// UseClient replaces the HTTP client; tests point it at a server of their own. It returns the function that restores it.
func UseClient(c *http.Client) func() {
	httpClient()
	prev := client
	client = c
	return func() { client = prev }
}

// VAPID returns the key pair of the hub, making and storing it when there is none.
func VAPID(ctx context.Context) (*webpush.Keys, error) {
	keysMu.Lock()
	defer keysMu.Unlock()
	if keys != nil {
		return keys, nil
	}
	_, all, err := system_model.GetAllSettings(ctx)
	if err != nil {
		return nil, err
	}
	if enc := all[vapidSettingKey]; enc != "" {
		if plain, err := secret.DecryptSecret(setting.SecretKey, enc); err == nil {
			if raw, err := webpush.DecodeKey(plain); err == nil {
				if k, err := webpush.KeysFromPrivate(raw); err == nil {
					keys = k
					return keys, nil
				}
			}
		}
		log.Error("asc push: the stored VAPID key cannot be read; making a new one (browsers subscribe again)")
	}
	k, err := webpush.NewKeys()
	if err != nil {
		return nil, err
	}
	enc, err := secret.EncryptSecret(setting.SecretKey, webpush.EncodeKey(k.Private()))
	if err != nil {
		return nil, err
	}
	if err := system_model.SetSettings(ctx, map[string]string{vapidSettingKey: enc}); err != nil {
		return nil, err
	}
	keys = k
	return keys, nil
}

// ResetKeysForTest forgets the cached key pair.
func ResetKeysForTest() {
	keysMu.Lock()
	keys = nil
	keysMu.Unlock()
}

// Message is what the browser shows. Path is a path of this hub, opened when the person clicks.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Path  string `json:"path,omitempty"`
	Tag   string `json:"tag,omitempty"`
}

func clip(s string) string {
	if len(s) <= maxTextBytes {
		return s
	}
	s = s[:maxTextBytes]
	for len(s) > 0 && s[len(s)-1]&0xC0 == 0x80 { // not in the middle of a character
		s = s[:len(s)-1]
	}
	return s
}

func subject() string {
	if strings.HasPrefix(setting.AppURL, "https://") {
		return strings.TrimSuffix(setting.AppURL, "/")
	}
	return "mailto:admin@" + setting.Domain
}

// Result is what became of one sending.
type Result int

const (
	Delivered Result = iota
	Gone             // the push service says the subscription does not exist any more
	Failed
)

// SendTo sends the message to one subscription and reports the result. It does not look at the choices of the person.
func SendTo(ctx context.Context, k *webpush.Keys, sub *user_model.AscPushSubscription, msg Message) (Result, error) {
	msg.Title, msg.Body = clip(msg.Title), clip(msg.Body)
	if msg.Path != "" && (!strings.HasPrefix(msg.Path, "/") || strings.HasPrefix(msg.Path, "//")) {
		msg.Path = ""
	}
	if msg.Path != "" && setting.AppSubURL != "" && !strings.HasPrefix(msg.Path, setting.AppSubURL+"/") {
		msg.Path = setting.AppSubURL + msg.Path
	}
	plain, err := json.Marshal(msg)
	if err != nil {
		return Failed, err
	}
	uaPublic, err := webpush.DecodeKey(sub.P256dh)
	if err != nil {
		return Gone, err
	}
	authSecret, err := webpush.DecodeKey(sub.Auth)
	if err != nil {
		return Gone, err
	}
	body, err := webpush.Encrypt(plain, uaPublic, authSecret, nil, nil)
	if err != nil {
		return Failed, err
	}
	authz, err := k.Authorization(sub.Endpoint, subject(), time.Now())
	if err != nil {
		return Gone, err
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Gone, err
	}
	req.Header.Set("Authorization", authz)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", "86400")
	req.Header.Set("Urgency", "normal")
	resp, err := httpClient().Do(req)
	if err != nil {
		return Failed, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return Delivered, nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return Gone, nil
	default:
		return Failed, fmt.Errorf("push service answered %d", resp.StatusCode)
	}
}

// Notify sends the event to every browser of the person if they want it now. It returns at once: the sending runs in
// the background, bounded, and a subscription the push service has dropped is removed.
func Notify(ctx context.Context, userID int64, event string, msg Message) {
	if !ascnotify.PushAllowed(ctx, userID, event, time.Now()) {
		return
	}
	subs, err := user_model.ListAscPushSubscriptions(ctx, userID)
	if err != nil || len(subs) == 0 {
		if err != nil {
			log.Error("asc push: subscriptions of user %d: %v", userID, err)
		}
		return
	}
	k, err := VAPID(ctx)
	if err != nil {
		log.Error("asc push: VAPID key: %v", err)
		return
	}
	for _, sub := range subs {
		select {
		case slots <- struct{}{}:
		default:
			log.Warn("asc push: too many sendings at once; dropping one for user %d", userID)
			continue
		}
		go func() {
			defer func() { <-slots }()
			bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*sendTimeout)
			defer cancel()
			res, err := SendTo(bg, k, sub, msg)
			switch res {
			case Gone:
				_ = user_model.DeleteAscPushSubscriptionByID(bg, sub.ID)
			case Failed:
				log.Warn("asc push: user %d: %v", userID, err)
			}
		}()
	}
}
