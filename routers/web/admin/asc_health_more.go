// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	gocontext "context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"

	"forgejo.org/modules/graceful"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/mailer"

	"golang.org/x/sys/unix"
)

// The checks of the health page beyond the database, the cache, the runners and the notices. The hub answers the disk,
// the outgoing mail, the TLS certificate and its own clock; asc serve answers its object store and its signing key
// (Maintenance.CheckHealth), and its clock is compared with the hub's.

// ---------------------------------------------------------------------------------------------- disk

// Disk thresholds: how full, and how soon it fills at the rate seen.
const (
	diskWarnPercent = 85
	diskErrPercent  = 95
	diskWarnDays    = 14
	diskErrDays     = 2
)

// The disk watch samples how many bytes the data volume uses, so the health page can say how soon it fills.
const (
	diskSampleEvery = 10 * time.Minute
	diskKeep        = 24 * time.Hour // the rate is taken over at most the last day
	diskMinSpan     = time.Hour      // and at least the last hour
)

type diskSample struct {
	at   time.Time
	used uint64
}

// diskHistory is the samples, oldest first. It knows what this process saw: a restart starts it again.
var diskHistory struct {
	sync.Mutex
	samples []diskSample
}

// recordDisk keeps a sample when the last one is at least diskSampleEvery old, and drops the ones older than diskKeep.
func recordDisk(now time.Time, used uint64) {
	diskHistory.Lock()
	defer diskHistory.Unlock()
	s := diskHistory.samples
	if n := len(s); n > 0 && now.Sub(s[n-1].at) < diskSampleEvery {
		return
	}
	s = append(s, diskSample{now, used})
	for len(s) > 0 && now.Sub(s[0].at) > diskKeep {
		s = s[1:]
	}
	diskHistory.samples = s
}

// diskFillDays is in how many days the volume is full at the rate it filled since the oldest sample, false when that
// sample is younger than diskMinSpan or the volume did not grow.
func diskFillDays(now time.Time, used, avail uint64) (float64, bool) {
	diskHistory.Lock()
	defer diskHistory.Unlock()
	if len(diskHistory.samples) == 0 {
		return 0, false
	}
	first := diskHistory.samples[0]
	span := now.Sub(first.at)
	if span < diskMinSpan || used <= first.used {
		return 0, false
	}
	perDay := float64(used-first.used) / span.Hours() * 24
	return float64(avail) / perDay, true
}

// diskBytes is the bytes in use and the bytes a user may still write on the file system holding path.
func diskBytes(path string) (used, avail uint64, ok bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	bs := uint64(st.Bsize) //nolint:gosec // a block size is positive
	if st.Bfree > st.Blocks {
		return 0, 0, false
	}
	return (st.Blocks - st.Bfree) * bs, st.Bavail * bs, true
}

var diskWatchOnce sync.Once

// InitDiskWatch starts sampling the data volume every diskSampleEvery until the hub stops.
func InitDiskWatch(gocontext.Context) error {
	diskWatchOnce.Do(func() {
		ctx := graceful.GetManager().ShutdownContext()
		go func() {
			t := time.NewTicker(diskSampleEvery)
			defer t.Stop()
			for {
				if used, _, ok := diskBytes(setting.AppDataPath); ok {
					recordDisk(time.Now(), used)
				}
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		}()
	})
	return nil
}

// diskRow is the Disk usage check for a volume that is pct full with avail bytes left, filling in days (known).
func diskRow(ctx *context.Context, pct int, days float64, known bool) checkRow {
	name := ctx.Tr("asc_admin.check_disk")
	text := ctx.Tr("asc_admin.check_disk_full", pct)
	if known {
		text = ctx.Tr("asc_admin.check_disk_rate", pct, strconv.Itoa(max(1, int(days+0.5))))
	}
	state := checkOK
	switch {
	case pct >= diskErrPercent || (known && days < diskErrDays):
		state = checkErr
	case pct >= diskWarnPercent || (known && days < diskWarnDays):
		state = checkWarn
	}
	return checkRow{State: state, Name: name, Text: text}
}

func checkDisk(ctx *context.Context) (checkRow, bool) {
	g := diskGauge(setting.AppDataPath)
	used, avail, ok := diskBytes(setting.AppDataPath)
	if !g.Known || !ok {
		return checkRow{}, false
	}
	now := time.Now()
	recordDisk(now, used)
	days, known := diskFillDays(now, used, avail)
	return diskRow(ctx, g.Percent, days, known), true
}

// ---------------------------------------------------------------------------------------------- mail

func checkMail(ctx *context.Context) (checkRow, bool) {
	q := mailer.QueueStatus()
	if !q.Enabled {
		return checkRow{}, false
	}
	r := mailRow(ctx, q, time.Now())
	row := checkRow{State: r.State, Name: ctx.Tr("asc_admin.check_mail"), Text: r.Text}
	if r.State != checkOK {
		row.Fix = setting.AppSubURL + "/admin/config"
	}
	return row, true
}

// ---------------------------------------------------------------------------------------------- TLS

// tlsWarnDays is how long before it expires the certificate the hub serves warns.
const tlsWarnDays = 14

// certExpiry is when the first certificate of a PEM file expires.
func certExpiry(path string) (time.Time, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	for {
		var blk *pem.Block
		blk, b = pem.Decode(b)
		if blk == nil {
			return time.Time{}, errors.New("no certificate in the file")
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			return time.Time{}, err
		}
		return c.NotAfter, nil
	}
}

// tlsRow is the TLS certificate check for a certificate that expires at notAfter.
func tlsRow(ctx *context.Context, notAfter, now time.Time) checkRow {
	name := ctx.Tr("asc_admin.check_tls")
	left := notAfter.Sub(now)
	day := notAfter.UTC().Format("2006-01-02")
	switch {
	case left <= 0:
		return checkRow{State: checkErr, Name: name, Text: ctx.Tr("asc_admin.check_tls_expired", day)}
	case left < tlsWarnDays*24*time.Hour:
		return checkRow{State: checkWarn, Name: name, Text: ctx.Tr("asc_admin.check_tls_soon", day, int(left.Hours()/24))}
	}
	return checkRow{State: checkOK, Name: name, Text: ctx.Tr("asc_admin.check_tls_ok", day, int(left.Hours()/24))}
}

func checkTLS(ctx *context.Context) (checkRow, bool) {
	// Behind a proxy that ends TLS the hub serves plain HTTP on loopback or a socket: the proxy's certificate is not
	// the hub's to check.
	if setting.Protocol != setting.HTTPS {
		return checkRow{}, false
	}
	if setting.EnableAcme {
		return checkRow{State: checkOK, Name: ctx.Tr("asc_admin.check_tls"), Text: ctx.Tr("asc_admin.check_tls_acme")}, true
	}
	if setting.CertFile == "" {
		return checkRow{}, false
	}
	notAfter, err := certExpiry(setting.CertFile)
	if err != nil {
		return checkRow{State: checkErr, Name: ctx.Tr("asc_admin.check_tls"), Text: ctx.Tr("asc_admin.check_tls_unread")}, true
	}
	return tlsRow(ctx, notAfter, time.Now()), true
}

// ---------------------------------------------------------------------------------------------- asc serve

// ascHealthTimeout bounds the call to asc serve: its object store probe gives up after 10 seconds.
var ascHealthTimeout = 12 * time.Second

// ascHealth asks asc serve to probe itself. ok is false when the check does not apply (asc off, or a server without
// the probes); err is set when asc serve did not answer.
func ascHealth(ctx *context.Context) (h asctrust.Health, ok bool, err error) {
	if !setting.Asc.Enabled || ctx.Doer == nil || !ctx.Doer.IsAdmin || !asctrust.HasFeature(ctx, asctrust.FeatureHealth) {
		return h, false, nil
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return h, true, err
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallInstance, nil)
	if err != nil {
		return h, true, err
	}
	defer release()
	callCtx, cancel := gocontext.WithTimeout(callCtx, ascHealthTimeout)
	defer cancel()
	h, err = asctrust.LoadHealth(callCtx, conn, time.Now)
	if err != nil {
		log.Debug("health: asc serve: %v", err)
	}
	return h, true, err
}

func checkObjectStore(ctx *context.Context) (checkRow, bool) {
	h, ok, err := ascHealth(ctx)
	if !ok {
		return checkRow{}, false
	}
	name := ctx.Tr("asc_admin.check_store")
	switch {
	case err != nil:
		return checkRow{State: checkNoRun, Name: name, Text: ctx.Tr("asc_admin.check_asc_unread")}, true
	case h.ObjectStore.OK():
		return checkRow{State: checkOK, Name: name, Text: ctx.Tr("asc_admin.check_store_ok", h.ObjectStore.Millis)}, true
	}
	return checkRow{State: checkErr, Name: name, Text: ctx.Tr("asc_admin.check_store_bad", h.ObjectStore.Detail)}, true
}

func checkSigningKey(ctx *context.Context) (checkRow, bool) {
	h, ok, err := ascHealth(ctx)
	if !ok {
		return checkRow{}, false
	}
	name := ctx.Tr("asc_admin.check_key")
	switch {
	case err != nil:
		return checkRow{State: checkNoRun, Name: name, Text: ctx.Tr("asc_admin.check_asc_unread")}, true
	case h.ServerKey.NotConfigured():
		return checkRow{State: checkWarn, Name: name, Text: ctx.Tr("asc_admin.check_key_none")}, true
	case h.ServerKey.OK():
		return checkRow{State: checkOK, Name: name, Text: ctx.Tr("asc_admin.check_key_ok", h.Fingerprint)}, true
	}
	return checkRow{State: checkErr, Name: name, Text: ctx.Tr("asc_admin.check_key_bad", h.ServerKey.Detail)}, true
}

// ---------------------------------------------------------------------------------------------- clock

// Clock thresholds: how far asc serve's clock may be from the hub's. Signed call tokens live for minutes, so a minute
// apart breaks calls; a few seconds is a clock that is not kept in time.
const (
	clockWarn = 2 * time.Second
	clockErr  = 30 * time.Second
)

// clockRow is the Clock check from what the host says (synced, known) and the difference to asc serve's clock (when
// skewKnown).
func clockRow(ctx *context.Context, synced, known bool, skew, uncertainty time.Duration, skewKnown bool) (checkRow, bool) {
	name := ctx.Tr("asc_admin.check_clock")
	apart := skew
	if apart < 0 {
		apart = -apart
	}
	apart = max(0, apart-uncertainty)
	shown := apart.Round(time.Millisecond).String()
	switch {
	case skewKnown && apart > clockErr:
		return checkRow{State: checkErr, Name: name, Text: ctx.Tr("asc_admin.check_clock_apart", shown)}, true
	case skewKnown && apart > clockWarn:
		return checkRow{State: checkWarn, Name: name, Text: ctx.Tr("asc_admin.check_clock_apart", shown)}, true
	case known && !synced:
		return checkRow{State: checkWarn, Name: name, Text: ctx.Tr("asc_admin.check_clock_unsynced")}, true
	case skewKnown:
		return checkRow{State: checkOK, Name: name, Text: ctx.Tr("asc_admin.check_clock_ok_asc", shown)}, true
	case known:
		return checkRow{State: checkOK, Name: name, Text: ctx.Tr("asc_admin.check_clock_ok")}, true
	}
	return checkRow{}, false
}

func checkClock(ctx *context.Context) (checkRow, bool) {
	synced, known := hostClockSynced()
	h, asked, err := ascHealth(ctx)
	return clockRow(ctx, synced, known, h.Skew, h.Uncertainty, asked && err == nil)
}
