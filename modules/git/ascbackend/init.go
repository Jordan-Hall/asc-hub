// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend

import (
	"context"
	"errors"

	"forgejo.org/modules/ascclient"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
)

// Init sets up the asc client from the [asc] settings when [asc] ENABLED is true, and does nothing
// otherwise. It makes no network call. A setting the client refuses (a plain-text http SERVER_URL on a
// non-loopback host, a missing internal token) is an error, so the server does not start with reads
// that would fail at the first request.
func Init(ctx context.Context) error {
	if !setting.Asc.Enabled {
		return nil
	}
	if setting.InternalToken == "" {
		return errors.New("ascbackend: [asc] ENABLED needs the internal token")
	}
	client, err := ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken})
	if err != nil {
		return err
	}
	Configure(client.Repo(), Options{})
	go func() {
		<-ctx.Done()
		Reset()
		if err := client.Close(); err != nil {
			log.Debug("ascbackend: closing the asc client: %v", err)
		}
	}()
	return nil
}
