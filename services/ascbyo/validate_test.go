// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewScope(t *testing.T) {
	for _, tc := range []struct {
		kind Kind
		name string
		ok   bool
		want string
	}{
		{KindUser, "Alice", true, "alice"},
		{KindUser, " bob_1.x-y ", true, "bob_1.x-y"},
		{KindOrg, "Org-1", true, "org-1"},
		{KindRepo, "Alice/Repo.Name", true, "alice/repo.name"},
		{KindUser, "", false, ""},
		{KindUser, "a/b", false, ""},
		{KindUser, "a b", false, ""},
		{KindUser, "a\x00b", false, ""},
		{KindUser, "é", false, ""},
		{KindRepo, "alice", false, ""},
		{KindRepo, "alice/", false, ""},
		{KindRepo, "/repo", false, ""},
		{KindRepo, "a/b/c", false, ""},
		{KindUser, strings.Repeat("a", MaxScopeNameBytes+1), false, ""},
		{0, "alice", false, ""},
	} {
		sc, err := NewScope(tc.kind, tc.name)
		if !tc.ok {
			assert.Error(t, err, "%v %q", tc.kind, tc.name)
			continue
		}
		require.NoError(t, err, "%v %q", tc.kind, tc.name)
		assert.Equal(t, tc.want, sc.Name)
		assert.True(t, sc.Valid())
	}
	assert.False(t, Scope{Kind: KindUser, Name: "Alice"}.Valid(), "a scope that was not normalised is invalid")
	assert.False(t, Scope{}.Valid())
}

func TestScopeOwns(t *testing.T) {
	u, _ := NewScope(KindUser, "alice")
	o, _ := NewScope(KindOrg, "alice")
	assert.True(t, u.Owns(u.Owner()))
	assert.True(t, u.Owns(&ascv1.OwnerRef{Kind: u.Owner().GetKind(), Name: "ALICE"}))
	assert.False(t, u.Owns(o.Owner()), "same name, other kind")
	assert.False(t, u.Owns(nil))
	assert.False(t, Scope{}.Owns(u.Owner()))
}

func TestNormalizeEndpoint(t *testing.T) {
	good := map[string]string{
		"https://s3.example.com":              "https://s3.example.com",
		"HTTPS://S3.Example.COM/":             "https://s3.example.com",
		"https://s3.example.com:9000":         "https://s3.example.com:9000",
		"https://s3.example.com/minio/":       "https://s3.example.com/minio",
		"  https://s3.example.com  ":          "https://s3.example.com",
		"https://[2001:db8::1]:443":           "https://[2001:db8::1]:443",
		"http://localhost:9000":               "http://localhost:9000", // asc decides on loopback http
		"https://s3.eu-central-1.example.com": "https://s3.eu-central-1.example.com",
	}
	for in, want := range good {
		got, ok := NormalizeEndpoint(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{
		"", "s3.example.com", "ftp://s3.example.com", "file:///etc/passwd", "gopher://x", "javascript:alert(1)",
		"https://", "https://user:pw@s3.example.com", "https://user@s3.example.com", "https://s3.example.com?x=1",
		"https://s3.example.com#frag", "https://s3.example.com/a b", "https://s3.example.com/a\\b", "https://s3.example.com:0",
		"https://s3.example.com:65536", "https://s3.example.com:abc", "https://s3.example.com//x", "https://s3.example.com/../x",
		"https://s3.exa\nmple.com", "https://s3.example.com\x00", "https://" + strings.Repeat("a", MaxEndpointBytes) + ".com",
		"https://é.example.com", "//s3.example.com", "https:s3.example.com",
	} {
		_, ok := NormalizeEndpoint(bad)
		assert.False(t, ok, "%q", bad)
	}
}

func TestNormalizePrefix(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "  ": "", "/": "", "a": "a/", "a/": "a/", "/a/b": "a/b/", "backups/asc.v1/": "backups/asc.v1/", "A_b-c": "A_b-c/",
	} {
		got, ok := NormalizePrefix(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"..", "a/../b", "./a", "a//b", "a b", "a\\b", "a\x00", "é/", strings.Repeat("a", MaxPrefixBytes+1), "a/./b"} {
		_, ok := NormalizePrefix(bad)
		assert.False(t, ok, "%q", bad)
	}
}

func goodStorage() StorageInput {
	return StorageInput{
		Name: "my-bucket", Endpoint: "https://s3.example.com", Region: "eu-west-1", Bucket: "my-bucket-1", Prefix: "asc",
		AccessKeyID: "AKIAEXAMPLEKEY", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
}

func TestStorageInputValidate(t *testing.T) {
	in := goodStorage()
	assert.Empty(t, in.Validate(true))
	assert.Equal(t, "asc/", in.Prefix, "prefix normalised")

	cases := map[string]func(*StorageInput){
		"name":              func(i *StorageInput) { i.Name = "Bad Name" },
		"name2":             func(i *StorageInput) { i.Name = "-x" },
		"name3":             func(i *StorageInput) { i.Name = strings.Repeat("a", 41) },
		"endpoint":          func(i *StorageInput) { i.Endpoint = "ftp://x" },
		"region":            func(i *StorageInput) { i.Region = "eu west" },
		"bucket":            func(i *StorageInput) { i.Bucket = "ab" },
		"bucket2":           func(i *StorageInput) { i.Bucket = "Has_Upper" },
		"bucket3":           func(i *StorageInput) { i.Bucket = "a..b" },
		"prefix":            func(i *StorageInput) { i.Prefix = "../x" },
		"access_key_id":     func(i *StorageInput) { i.AccessKeyID = "" },
		"access_key_id2":    func(i *StorageInput) { i.AccessKeyID = "has space" },
		"secret_access_key": func(i *StorageInput) { i.SecretAccessKey = "" },
		"secret_access_key2": func(i *StorageInput) {
			i.SecretAccessKey = strings.Repeat("s", MaxSecretKeyBytes+1)
		},
		"session_token": func(i *StorageInput) { i.SessionToken = "a b" },
	}
	for name, mutate := range cases {
		in := goodStorage()
		mutate(&in)
		fe := in.Validate(true)
		field := strings.TrimRight(name, "0123456789")
		assert.Contains(t, fe, field, name)
		assert.NotEmpty(t, fe[field], name)
	}

	t.Run("a paste adds whitespace around a secret", func(t *testing.T) {
		in := goodStorage()
		in.SecretAccessKey = "  " + in.SecretAccessKey + "\r\n"
		assert.Empty(t, in.Validate(true))
		assert.NotContains(t, in.SecretAccessKey, "\n")
	})

	t.Run("edit: credentials optional but paired", func(t *testing.T) {
		in := goodStorage()
		in.AccessKeyID, in.SecretAccessKey = "", ""
		assert.Empty(t, in.Validate(false))
		in.AccessKeyID = "AKIAEXAMPLEKEY"
		assert.Contains(t, in.Validate(false), "secret_access_key")
		in.AccessKeyID, in.SecretAccessKey = "", "onlysecret"
		assert.Contains(t, in.Validate(false), "access_key_id")
		in.SessionToken = "tok"
		assert.True(t, in.HasCredentials())
	})
}

func TestAIKeyInputValidate(t *testing.T) {
	good := func() AIKeyInput {
		return AIKeyInput{Label: "Team key", Provider: "anthropic", APIKey: "sk-ant-api03-abcdefghijkl", DefaultModel: "claude-sonnet-4-5", CapUSD: "25.50"}
	}
	in := good()
	assert.Empty(t, in.Validate(true))
	assert.Equal(t, uint64(25_500_000), in.CapMicro)

	for name, mutate := range map[string]func(*AIKeyInput){
		"label":         func(i *AIKeyInput) { i.Label = "" },
		"label2":        func(i *AIKeyInput) { i.Label = strings.Repeat("a", MaxLabelBytes+1) },
		"label3":        func(i *AIKeyInput) { i.Label = "a\x00b" },
		"provider":      func(i *AIKeyInput) { i.Provider = "skynet" },
		"api_key":       func(i *AIKeyInput) { i.APIKey = "short" },
		"api_key2":      func(i *AIKeyInput) { i.APIKey = "has space in the key" },
		"api_key3":      func(i *AIKeyInput) { i.APIKey = strings.Repeat("k", MaxAPIKeyBytes+1) },
		"api_key4":      func(i *AIKeyInput) { i.APIKey = "key-with-é-character" },
		"base_url":      func(i *AIKeyInput) { i.BaseURL = "file:///x" },
		"base_url2":     func(i *AIKeyInput) { i.BaseURL = "https://u:p@api.example.com" },
		"default_model": func(i *AIKeyInput) { i.DefaultModel = "bad model" },
		"monthly_cap":   func(i *AIKeyInput) { i.CapUSD = "-5" },
		"monthly_cap2":  func(i *AIKeyInput) { i.CapUSD = "1e3" },
		"monthly_cap3":  func(i *AIKeyInput) { i.CapUSD = "1.1234567" },
		"monthly_cap4":  func(i *AIKeyInput) { i.CapUSD = "1000001" },
	} {
		in := good()
		mutate(&in)
		field := strings.TrimRight(name, "0123456789")
		assert.Contains(t, in.Validate(true), field, name)
	}

	t.Run("openai-compatible needs a base url on create", func(t *testing.T) {
		in := good()
		in.Provider = "openai_compatible"
		assert.Contains(t, in.Validate(true), "base_url")
		in.BaseURL = "https://llm.example.com/v1/"
		assert.Empty(t, in.Validate(true))
		assert.Equal(t, "https://llm.example.com/v1", in.BaseURL)
	})

	t.Run("edit: empty key keeps the stored one", func(t *testing.T) {
		in := good()
		in.APIKey = ""
		assert.Empty(t, in.Validate(false))
		in.APIKey = "bad"
		assert.Contains(t, in.Validate(false), "api_key")
	})
}

func TestUSD(t *testing.T) {
	for in, want := range map[string]uint64{
		"": 0, "0": 0, "0.00": 0, "1": 1_000_000, "$25": 25_000_000, "12.5": 12_500_000, "0.000001": 1, ".5": 500_000,
		"1000000": 1_000_000_000_000, "1000000.0": 1_000_000_000_000, "7.": 7_000_000,
	} {
		got, ok := ParseUSD(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"-1", "+1", "1,000", "1e3", "NaN", "inf", "1.2.3", "$", ".", "0.0000001", "1000000.01", "99999999", "abc", "1 2", "١٢"} {
		_, ok := ParseUSD(bad)
		assert.False(t, ok, bad)
	}
	for in, want := range map[uint64]string{
		0: "$0.00", 4200: "$0.0042", 1: "$0.000001", 12_500_000: "$12.50", 25_000_000: "$25.00", 1_234_567_890_000: "$1,234,567.89",
		999_999: "$0.999999", 10_000: "$0.01",
	} {
		assert.Equal(t, want, FormatUSD(in), in)
	}
	for in, want := range map[uint64]string{0: "", 25_000_000: "25", 12_500_000: "12.5", 1: "0.000001"} {
		assert.Equal(t, want, CapUSDInput(in))
	}
	for in, want := range map[uint64]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", 100000: "100,000"} {
		assert.Equal(t, want, FormatCount(in))
	}
}

func TestIDs(t *testing.T) {
	assert.True(t, ValidTargetID("st_abcdefghijklmnopqrst"))
	assert.False(t, ValidTargetID("st_abcdefghijklmnopqrs"))
	assert.False(t, ValidTargetID("ak_abcdefghijklmnopqrst"))
	assert.False(t, ValidTargetID("st_abcdefghijklmnopqrs!"))
	assert.False(t, ValidTargetID("st_abcdefghijklmnopqrst\n"))
	assert.True(t, ValidKeyID("ak_abcdefghijklmnopqrst"))
	assert.False(t, ValidKeyID("st_abcdefghijklmnopqrst"))
	assert.True(t, ValidMigrationID("mg_abc-123"))
	assert.False(t, ValidMigrationID(""))
	assert.False(t, ValidMigrationID("../x"))
	assert.False(t, ValidMigrationID(strings.Repeat("a", 65)))
}
