// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pwn

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rangeTransport struct{ paths *[]string }

func (r rangeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	*r.paths = append(*r.paths, req.URL.Path+" "+req.Header.Get("Add-Padding"))
	res := &http.Response{Request: req, Header: make(http.Header), StatusCode: http.StatusOK}
	switch req.URL.Path {
	case "/range/A1733":
		res.Body = io.NopCloser(strings.NewReader("c4ce0f1f0062b27b9e2f41af0c08218017c:1\r\nFC446EB88938834178CB9322C1EE273C2A7:2\r\nFE81480327C992FE62065A827429DD1318B:0\r\nSHORT:5\r\nnot a pair"))
	default:
		res.StatusCode = http.StatusServiceUnavailable
		res.Body = io.NopCloser(strings.NewReader(""))
	}
	return res, nil
}

func TestRange(t *testing.T) {
	var paths []string
	c := New(WithHTTP(&http.Client{Transport: rangeTransport{&paths}}))

	t.Run("Suffixes", func(t *testing.T) {
		got, err := c.Range("a1733")
		require.NoError(t, err)
		// upper case, the padding (count 0) and the lines that are not a suffix left out
		assert.Equal(t, []string{"C4CE0F1F0062B27B9E2F41AF0C08218017C", "FC446EB88938834178CB9322C1EE273C2A7"}, got)
		assert.Equal(t, []string{"/range/A1733 true"}, paths)
	})

	t.Run("BadPrefix", func(t *testing.T) {
		for _, p := range []string{"", "a173", "a17333", "g1733", "a17 3", "../x"} {
			_, err := c.Range(p)
			assert.ErrorIs(t, err, ErrBadPrefix, p)
		}
	})

	t.Run("ServiceFails", func(t *testing.T) {
		_, err := c.Range("FFFFF")
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrBadPrefix)
	})
}
