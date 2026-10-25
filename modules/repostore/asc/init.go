// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asc

import (
	"context"
	"errors"

	"forgejo.org/modules/ascclient"
	"forgejo.org/modules/log"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/setting"
)

// Init sets up the asc client from the [asc] settings when [asc] ENABLED is true, and does nothing otherwise.
// It makes no network call. A setting the client refuses (a plain-text http SERVER_URL on a non-loopback host,
// a missing internal token) is an error, so the server does not start with writes that would fail at the
// first request. Until Init has run, a request for an asc repository fails with ErrNotConfigured; it never
// falls back to the git Writer.
func Init(ctx context.Context) error {
	if !setting.Asc.Enabled {
		return nil
	}
	if setting.InternalToken == "" {
		return errors.New("repostore/asc: [asc] ENABLED needs the internal token")
	}
	client, err := ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken})
	if err != nil {
		return err
	}
	Configure(client.Repo(), Options{})
	repostore.UseAscWriter(setting.Asc.Writer == setting.AscWriterAsc)
	go func() {
		<-ctx.Done()
		repostore.UseAscWriter(false)
		Reset()
		if err := client.Close(); err != nil {
			log.Debug("repostore/asc: closing the asc client: %v", err)
		}
	}()
	return nil
}
