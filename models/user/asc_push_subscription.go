// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/netip"
	"net/url"
	"strings"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
)

// AscMaxPushSubscriptions is how many browsers a person can have subscribed at once.
const AscMaxPushSubscriptions = 10

// AscPushSubscription is one browser that a person allowed to show notifications of the hub (Web Push, RFC 8030).
// Endpoint, P256dh and Auth are what the browser gave; they are secrets of the person and are never shown to others.
type AscPushSubscription struct {
	ID           int64              `xorm:"pk autoincr"`
	UserID       int64              `xorm:"INDEX NOT NULL"`
	EndpointHash string             `xorm:"VARCHAR(64) UNIQUE NOT NULL"`
	Endpoint     string             `xorm:"TEXT NOT NULL"`
	P256dh       string             `xorm:"VARCHAR(128) NOT NULL"`
	Auth         string             `xorm:"VARCHAR(32) NOT NULL"`
	CreatedUnix  timeutil.TimeStamp `xorm:"created"`
}

func init() {
	db.RegisterModel(new(AscPushSubscription))
}

// TableName is the table of the model.
func (AscPushSubscription) TableName() string { return "asc_push_subscription" }

// ErrTooManyPushSubscriptions is returned when the person has the most subscriptions already.
var ErrTooManyPushSubscriptions = errors.New("too many subscribed browsers")

// ascEndpointHash identifies an endpoint without indexing a long text.
func ascEndpointHash(endpoint string) string {
	sum := sha256.Sum256([]byte(endpoint))
	return hex.EncodeToString(sum[:])
}

// ValidateAscPushEndpoint refuses an endpoint the hub must never call: not https, with a login in it, a very long one,
// or an address of this machine or network. The sender checks the address again when it connects.
func ValidateAscPushEndpoint(endpoint string) error {
	if len(endpoint) == 0 || len(endpoint) > 1000 {
		return errors.New("endpoint has a wrong length")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("endpoint must be an https URL")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return errors.New("endpoint is not a public address")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() {
			return errors.New("endpoint is not a public address")
		}
	}
	return nil
}

// SaveAscPushSubscription stores a subscription of the person. A browser that subscribes again replaces its old
// entry, also when that belonged to another person who used the same browser.
func SaveAscPushSubscription(ctx context.Context, userID int64, endpoint, p256dh, auth string) error {
	if err := ValidateAscPushEndpoint(endpoint); err != nil {
		return err
	}
	h := ascEndpointHash(endpoint)
	return db.WithTx(ctx, func(ctx context.Context) error {
		e := db.GetEngine(ctx)
		if _, err := e.Where("endpoint_hash = ?", h).Delete(new(AscPushSubscription)); err != nil {
			return err
		}
		n, err := e.Where("user_id = ?", userID).Count(new(AscPushSubscription))
		if err != nil {
			return err
		}
		if n >= AscMaxPushSubscriptions {
			return ErrTooManyPushSubscriptions
		}
		_, err = e.Insert(&AscPushSubscription{UserID: userID, EndpointHash: h, Endpoint: endpoint, P256dh: p256dh, Auth: auth})
		return err
	})
}

// ListAscPushSubscriptions returns the subscriptions of the person.
func ListAscPushSubscriptions(ctx context.Context, userID int64) ([]*AscPushSubscription, error) {
	var subs []*AscPushSubscription
	return subs, db.GetEngine(ctx).Where("user_id = ?", userID).OrderBy("id").Find(&subs)
}

// DeleteAscPushSubscription removes the subscription of the person with the endpoint; it reports whether one existed.
func DeleteAscPushSubscription(ctx context.Context, userID int64, endpoint string) (bool, error) {
	n, err := db.GetEngine(ctx).Where("user_id = ? AND endpoint_hash = ?", userID, ascEndpointHash(endpoint)).Delete(new(AscPushSubscription))
	return n > 0, err
}

// DeleteAscPushSubscriptionByID removes a subscription the push service said is gone.
func DeleteAscPushSubscriptionByID(ctx context.Context, id int64) error {
	_, err := db.GetEngine(ctx).ID(id).Delete(new(AscPushSubscription))
	return err
}

// DeleteAscPushSubscriptionsOfUser removes all subscriptions of a person, e.g. when the account is deleted.
func DeleteAscPushSubscriptionsOfUser(ctx context.Context, userID int64) error {
	_, err := db.GetEngine(ctx).Where("user_id = ?", userID).Delete(new(AscPushSubscription))
	return err
}
