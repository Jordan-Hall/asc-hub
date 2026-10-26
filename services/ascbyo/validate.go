// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// Limits of the inputs. They follow storage.proto and aiproviders.proto; asc checks them again.
const (
	MaxEndpointBytes     = 2048
	MaxRegionBytes       = 64
	MaxPrefixBytes       = 512
	MaxAccessKeyIDBytes  = 128
	MaxSecretKeyBytes    = 256
	MaxSessionTokenBytes = 4096
	MinAPIKeyBytes       = 8
	MaxAPIKeyBytes       = 512
	MaxLabelBytes        = 64
	MaxModelBytes        = 128
	MaxCapUSD            = 1_000_000
	// MaxFormBytes bounds a whole settings form.
	MaxFormBytes = 32 << 10
)

// FieldErrors maps a form field to the translation key of what is wrong with it.
type FieldErrors map[string]string

// Add records the first error of a field.
func (fe FieldErrors) Add(field, key string) {
	if _, ok := fe[field]; !ok {
		fe[field] = key
	}
}

var (
	targetNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	bucketRE     = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	regionRE     = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	prefixRE     = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*/$`)
	modelRE      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,127}$`)
	targetIDRE   = regexp.MustCompile(`^st_[A-Za-z0-9]{20}$`)
	keyIDRE      = regexp.MustCompile(`^ak_[A-Za-z0-9]{20}$`)
	migrationRE  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// ValidTargetID, ValidKeyID and ValidMigrationID check an id taken from a URL before it is sent anywhere.
func ValidTargetID(id string) bool    { return targetIDRE.MatchString(id) }
func ValidKeyID(id string) bool       { return keyIDRE.MatchString(id) }
func ValidMigrationID(id string) bool { return migrationRE.MatchString(id) }

// visibleASCII reports whether s is non-empty printable ASCII without spaces.
func visibleASCII(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if c := s[i]; c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

// TrimSecret removes the whitespace a paste adds around a secret. Whitespace inside stays and fails the
// check.
func TrimSecret(s string) string { return strings.Trim(s, " \t\r\n") }

// NormalizeEndpoint checks an S3 endpoint URL and returns it in a canonical form. It is a convenience check
// for the form: the address guard (scheme, resolved addresses, ports) runs in asc, which is the authority.
func NormalizeEndpoint(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > MaxEndpointBytes || strings.ContainsAny(raw, "?#\\ \t\r\n") {
		return "", false
	}
	for i := range len(raw) {
		if raw[i] < 0x21 || raw[i] > 0x7e {
			return "", false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", false
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", false
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", false
		}
	}
	if strings.Contains(u.Path, "//") || strings.Contains(u.Path, "..") {
		return "", false
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), true
}

// NormalizePrefix returns the prefix as asc wants it: "" or "a/b/".
func NormalizePrefix(raw string) (string, bool) {
	p := strings.TrimSpace(raw)
	p = strings.TrimLeft(p, "/")
	if p == "" {
		return "", true
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	if len(p) > MaxPrefixBytes || !prefixRE.MatchString(p) {
		return "", false
	}
	for seg := range strings.SplitSeq(p, "/") {
		if seg == ".." || seg == "." {
			return "", false
		}
	}
	return p, true
}

// AddressingFromForm maps the form value.
func AddressingFromForm(v string) (ascv1.StorageAddressing, bool) {
	switch v {
	case "", "auto":
		return ascv1.StorageAddressing_STORAGE_ADDRESSING_AUTO, true
	case "path":
		return ascv1.StorageAddressing_STORAGE_ADDRESSING_PATH, true
	case "virtual_host":
		return ascv1.StorageAddressing_STORAGE_ADDRESSING_VIRTUAL_HOST, true
	}
	return 0, false
}

// AddressingToForm is the inverse of AddressingFromForm.
func AddressingToForm(a ascv1.StorageAddressing) string {
	switch a {
	case ascv1.StorageAddressing_STORAGE_ADDRESSING_PATH:
		return "path"
	case ascv1.StorageAddressing_STORAGE_ADDRESSING_VIRTUAL_HOST:
		return "virtual_host"
	}
	return "auto"
}

// StorageInput is what the add and edit forms carry. The credential fields are write-only: they are never
// copied into a view.
type StorageInput struct {
	Name, Endpoint, Region, Bucket, Prefix string
	Addressing                             ascv1.StorageAddressing
	AccessKeyID                            string
	SecretAccessKey                        string
	SessionToken                           string
}

// HasCredentials reports whether any credential field was filled in.
func (in *StorageInput) HasCredentials() bool {
	return in.AccessKeyID != "" || in.SecretAccessKey != "" || in.SessionToken != ""
}

// secrets lists the values to scrub from messages.
func (in *StorageInput) secrets() []string {
	return []string{in.AccessKeyID, in.SecretAccessKey, in.SessionToken}
}

// Validate normalises the input in place and returns the errors by field. On create the credentials are
// required; on edit they are optional, but key id and secret then come together.
func (in *StorageInput) Validate(create bool) FieldErrors {
	fe := FieldErrors{}
	in.Name = strings.ToLower(strings.TrimSpace(in.Name))
	if !targetNameRE.MatchString(in.Name) {
		fe.Add("name", "ascbyo.storage.err.name")
	}
	if ep, ok := NormalizeEndpoint(in.Endpoint); ok {
		in.Endpoint = ep
	} else {
		fe.Add("endpoint", "ascbyo.storage.err.endpoint")
	}
	in.Region = strings.TrimSpace(in.Region)
	if in.Region != "" && !regionRE.MatchString(in.Region) {
		fe.Add("region", "ascbyo.storage.err.region")
	}
	in.Bucket = strings.TrimSpace(in.Bucket)
	if !bucketRE.MatchString(in.Bucket) || strings.Contains(in.Bucket, "..") {
		fe.Add("bucket", "ascbyo.storage.err.bucket")
	}
	if p, ok := NormalizePrefix(in.Prefix); ok {
		in.Prefix = p
	} else {
		fe.Add("prefix", "ascbyo.storage.err.prefix")
	}
	in.AccessKeyID = TrimSecret(in.AccessKeyID)
	in.SecretAccessKey = TrimSecret(in.SecretAccessKey)
	in.SessionToken = TrimSecret(in.SessionToken)
	if create || in.HasCredentials() {
		if !visibleASCII(in.AccessKeyID) || len(in.AccessKeyID) > MaxAccessKeyIDBytes {
			fe.Add("access_key_id", "ascbyo.storage.err.access_key_id")
		}
		if !visibleASCII(in.SecretAccessKey) || len(in.SecretAccessKey) > MaxSecretKeyBytes {
			fe.Add("secret_access_key", "ascbyo.storage.err.secret_access_key")
		}
	}
	if in.SessionToken != "" && (!visibleASCII(in.SessionToken) || len(in.SessionToken) > MaxSessionTokenBytes) {
		fe.Add("session_token", "ascbyo.storage.err.session_token")
	}
	return fe
}

// Provider is the form name of an AI provider.
type Provider string

// The providers the form offers.
const (
	ProviderAnthropic        Provider = "anthropic"
	ProviderOpenAI           Provider = "openai"
	ProviderGoogle           Provider = "google"
	ProviderOpenAICompatible Provider = "openai_compatible"
)

// Providers lists the providers in the order the form shows them.
var Providers = []Provider{ProviderAnthropic, ProviderOpenAI, ProviderGoogle, ProviderOpenAICompatible}

// ProviderFromForm maps the form value to the wire enum.
func ProviderFromForm(v string) (ascv1.AiProvider, bool) {
	switch Provider(v) {
	case ProviderAnthropic:
		return ascv1.AiProvider_AI_PROVIDER_ANTHROPIC, true
	case ProviderOpenAI:
		return ascv1.AiProvider_AI_PROVIDER_OPENAI, true
	case ProviderGoogle:
		return ascv1.AiProvider_AI_PROVIDER_GOOGLE, true
	case ProviderOpenAICompatible:
		return ascv1.AiProvider_AI_PROVIDER_OPENAI_COMPATIBLE, true
	}
	return 0, false
}

// ProviderToForm is the inverse of ProviderFromForm; an unknown provider gives "".
func ProviderToForm(p ascv1.AiProvider) Provider {
	switch p {
	case ascv1.AiProvider_AI_PROVIDER_ANTHROPIC:
		return ProviderAnthropic
	case ascv1.AiProvider_AI_PROVIDER_OPENAI:
		return ProviderOpenAI
	case ascv1.AiProvider_AI_PROVIDER_GOOGLE:
		return ProviderGoogle
	case ascv1.AiProvider_AI_PROVIDER_OPENAI_COMPATIBLE:
		return ProviderOpenAICompatible
	}
	return ""
}

// AIKeyInput is what the add and edit forms of an AI key carry. APIKey is write-only.
type AIKeyInput struct {
	Label        string
	Provider     string
	APIKey       string
	BaseURL      string
	DefaultModel string
	// CapUSD is the monthly cap as typed ("" and "0" mean none).
	CapUSD string
	// CapMicro is CapUSD parsed by Validate.
	CapMicro uint64
}

func (in *AIKeyInput) secrets() []string { return []string{in.APIKey} }

// Validate normalises the input in place and returns the errors by field. On create the key is required;
// on edit an empty key keeps the stored one.
func (in *AIKeyInput) Validate(create bool) FieldErrors {
	fe := FieldErrors{}
	in.Label = strings.TrimSpace(in.Label)
	if in.Label == "" || len(in.Label) > MaxLabelBytes || hasControl(in.Label) {
		fe.Add("label", "ascbyo.ai.err.label")
	}
	if create {
		if _, ok := ProviderFromForm(in.Provider); !ok {
			fe.Add("provider", "ascbyo.ai.err.provider")
		}
	}
	in.APIKey = TrimSecret(in.APIKey)
	if create || in.APIKey != "" {
		if len(in.APIKey) < MinAPIKeyBytes || len(in.APIKey) > MaxAPIKeyBytes || !visibleASCII(in.APIKey) {
			fe.Add("api_key", "ascbyo.ai.err.api_key")
		}
	}
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	if in.BaseURL != "" {
		if u, ok := NormalizeEndpoint(in.BaseURL); ok {
			in.BaseURL = u
		} else {
			fe.Add("base_url", "ascbyo.ai.err.base_url")
		}
	} else if Provider(in.Provider) == ProviderOpenAICompatible && create {
		fe.Add("base_url", "ascbyo.ai.err.base_url_required")
	}
	in.DefaultModel = strings.TrimSpace(in.DefaultModel)
	if in.DefaultModel != "" && !modelRE.MatchString(in.DefaultModel) {
		fe.Add("default_model", "ascbyo.ai.err.default_model")
	}
	if m, ok := ParseUSD(in.CapUSD); ok {
		in.CapMicro = m
	} else {
		fe.Add("monthly_cap", "ascbyo.ai.err.cap")
	}
	return fe
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return true
		}
	}
	return false
}

// ParseUSD parses a dollar amount typed in a form into micro-dollars. It accepts "", digits, an optional
// "$" and up to six decimals; no sign, exponent or separators. The result is at most MaxCapUSD dollars.
func ParseUSD(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, true
	}
	s = strings.TrimPrefix(s, "$")
	if s == "" {
		return 0, false
	}
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" && frac == "" {
		return 0, false
	}
	if len(whole) > 7 || len(frac) > 6 {
		return 0, false
	}
	for _, part := range []string{whole, frac} {
		for i := range len(part) {
			if part[i] < '0' || part[i] > '9' {
				return 0, false
			}
		}
	}
	var w uint64
	if whole != "" {
		n, err := strconv.ParseUint(whole, 10, 64)
		if err != nil {
			return 0, false
		}
		w = n
	}
	frac += strings.Repeat("0", 6-len(frac))
	f, err := strconv.ParseUint(frac, 10, 64)
	if err != nil {
		return 0, false
	}
	if w > MaxCapUSD || (w == MaxCapUSD && f > 0) {
		return 0, false
	}
	return w*1_000_000 + f, true
}

// FormatUSD prints micro-dollars as "$12.50", with more decimals (up to six) only when they carry value.
func FormatUSD(micro uint64) string {
	whole, frac := micro/1_000_000, micro%1_000_000
	f := strconv.FormatUint(frac+1_000_000, 10)[1:]
	f = strings.TrimRight(f, "0")
	for len(f) < 2 {
		f += "0"
	}
	return "$" + FormatCount(whole) + "." + f
}

// FormatCount prints n with thousands separators.
func FormatCount(n uint64) string {
	s := strconv.FormatUint(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// CapUSDInput prints micro-dollars for a form field: "" for no cap, else "25" or "25.5".
func CapUSDInput(micro uint64) string {
	if micro == 0 {
		return ""
	}
	whole, frac := micro/1_000_000, micro%1_000_000
	if frac == 0 {
		return strconv.FormatUint(whole, 10)
	}
	f := strings.TrimRight(strconv.FormatUint(frac+1_000_000, 10)[1:], "0")
	return strconv.FormatUint(whole, 10) + "." + f
}
