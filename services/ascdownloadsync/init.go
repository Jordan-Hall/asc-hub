// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascdownloadsync

import (
	"context"
	"fmt"
	"sync/atomic"

	"forgejo.org/modules/setting"
)

var active atomic.Pointer[Syncer]

// Init starts the syncer when [asc] DOWNLOAD_SOURCE_URL is set. The first run and the later ones happen in a
// goroutine of their own, so a slow or unreachable source never holds up the start of the hub; the goroutine ends
// with ctx. A setting that cannot be used stops the start, like the other [asc] settings.
func Init(ctx context.Context) error {
	if setting.Asc.DownloadSourceURL == "" {
		return nil
	}
	s, err := New(Config{
		SourceURL: setting.Asc.DownloadSourceURL, Token: setting.Asc.DownloadSourceToken,
		PublicKey: setting.Asc.DownloadPublicKey, Dir: setting.Asc.DownloadDir,
	}, nil)
	if err != nil {
		return fmt.Errorf("[asc] download source: %w", err)
	}
	active.Store(s)
	go func() {
		defer active.CompareAndSwap(s, nil)
		s.Run(ctx, setting.Asc.DownloadSyncInterval)
	}()
	return nil
}

// Status reports the last good release and the error of the last run for the syncer of this process. configured is
// false when the hub does not fetch a release.
func Status() (last Result, lastErr error, configured bool) {
	s := active.Load()
	if s == nil {
		return Result{}, nil, false
	}
	last, lastErr = s.Last()
	return last, lastErr, true
}
