// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascforward

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discoveryRig(t *testing.T, answer func(w http.ResponseWriter)) (*Discovery, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "/.well-known/asc", r.URL.Path)
		answer(w)
	}))
	t.Cleanup(srv.Close)
	d, err := NewDiscovery(srv.URL+"/", http.DefaultTransport)
	require.NoError(t, err)
	return d, &calls
}

func TestDiscoverySeesTheSeedsOfAscAndKeepsTheAnswer(t *testing.T) {
	d, calls := discoveryRig(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"server":"https://asc.test","seeds":"https://asc.test/seeds","repos":"*"}`))
	})
	assert.True(t, d.Seeds(t.Context()))
	assert.True(t, d.Seeds(t.Context()))
	assert.Equal(t, int32(1), calls.Load(), "the answer is kept")

	d.mu.Lock()
	d.until = time.Now().Add(-time.Second)
	d.mu.Unlock()
	assert.True(t, d.Seeds(t.Context()))
	assert.Equal(t, int32(2), calls.Load(), "asked again once it is old")
}

func TestDiscoveryWithoutSeedsOrWithAFailureSaysNo(t *testing.T) {
	for name, answer := range map[string]func(w http.ResponseWriter){
		"no seeds":  func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{"server":"https://asc.test","repos":"*"}`)) },
		"not found": func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotFound) },
		"failure":   func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) },
		"not json":  func(w http.ResponseWriter) { _, _ = w.Write([]byte("<html>")) },
		"too large": func(w http.ResponseWriter) {
			_, _ = w.Write([]byte(`{"seeds":"` + strings.Repeat("a", MaxDiscoveryBytes) + `"}`))
		},
		"a redirect": func(w http.ResponseWriter) {
			w.Header().Set("Location", "https://elsewhere.test/")
			w.WriteHeader(http.StatusFound)
		},
	} {
		t.Run(name, func(t *testing.T) {
			d, calls := discoveryRig(t, answer)
			assert.False(t, d.Seeds(t.Context()))
			assert.False(t, d.Seeds(t.Context()))
			assert.Equal(t, int32(1), calls.Load(), "a failure is kept too, so a clone is not held up")
		})
	}
}

func TestDiscoveryOfNoServerSaysNo(t *testing.T) {
	var d *Discovery
	assert.False(t, d.Seeds(t.Context()))
	_, err := NewDiscovery("ftp://asc.test", nil)
	require.Error(t, err)
	_, err = NewDiscovery("https://user:pw@asc.test", nil)
	require.Error(t, err)
}
