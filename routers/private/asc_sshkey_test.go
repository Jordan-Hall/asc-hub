// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	asymkey_model "forgejo.org/models/asymkey"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/json"
	"forgejo.org/modules/private"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// ascNewSSHKey makes a new ed25519 key. It returns the authorized_keys text that
// Forgejo stores and the request that asc sends for it.
func ascNewSSHKey(t *testing.T) (string, *private.AscSSHKeyRequest) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pk, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pk))), ascSSHRequestFor(t, pk)
}

// ascAddUserKey stores content as a user key of the given owner.
func ascAddUserKey(t *testing.T, ownerID int64, content string) *asymkey_model.PublicKey {
	t.Helper()
	pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(content))
	require.NoError(t, err)
	key, err := asymkey_model.AddPublicKey(db.DefaultContext, ownerID, "asc-"+ssh.FingerprintSHA256(pk), content, 0)
	require.NoError(t, err)
	return key
}

func ascSSHRequestFor(t *testing.T, pk ssh.PublicKey) *private.AscSSHKeyRequest {
	t.Helper()
	return &private.AscSSHKeyRequest{
		Type:        pk.Type(),
		Key:         base64.StdEncoding.EncodeToString(pk.Marshal()),
		Fingerprint: ssh.FingerprintSHA256(pk),
	}
}

// ascSSHRequestOf builds the request for stored authorized_keys text.
func ascSSHRequestOf(t *testing.T, content string) *private.AscSSHKeyRequest {
	t.Helper()
	pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(content))
	require.NoError(t, err)
	return ascSSHRequestFor(t, pk)
}

func ascSSHKeyCall(t *testing.T, body string) (int, string) {
	t.Helper()
	ctx, resp := contexttest.MockPrivateContext(t, "POST /api/internal/asc/ssh-key")
	ctx.Req.Method = http.MethodPost
	ctx.Req.Body = io.NopCloser(strings.NewReader(body))
	AscSSHKey(ctx)
	return resp.Code, resp.Body.String()
}

func ascSSHKeyDo(t *testing.T, req *private.AscSSHKeyRequest) private.AscSSHKeyResponse {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	code, out := ascSSHKeyCall(t, string(body))
	require.Equal(t, http.StatusOK, code, out)
	var resp private.AscSSHKeyResponse
	require.NoError(t, json.Unmarshal([]byte(out), &resp))
	return resp
}

func ascSSHAuthz(t *testing.T, key *private.AscSSHKeyRequest, repo, mode string) private.AscAuthzResponse {
	t.Helper()
	return ascAuthzDo(t, private.AscAuthzRequest{SSHKey: key, Owner: "user2", Repo: repo, Mode: mode})
}

func TestAscSSHKeyDisabled(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, false)()

	_, req := ascNewSSHKey(t)
	body, err := json.Marshal(req)
	require.NoError(t, err)
	code, _ := ascSSHKeyCall(t, string(body))
	assert.Equal(t, http.StatusNotFound, code)

	// The authz endpoint is off as well.
	code, _ = ascAuthzCall(t, `{"owner":"user2","repo":"repo1","mode":"read"}`)
	assert.Equal(t, http.StatusNotFound, code)
}

func TestAscSSHKeyInternalToken(t *testing.T) {
	ascAuthzSetup(t)
	defer test.MockVariableValue(&setting.InternalToken, "internal-secret")()

	_, req := ascNewSSHKey(t)
	body, err := json.Marshal(req)
	require.NoError(t, err)

	do := func(authorization string) int {
		httpReq := httptest.NewRequest(http.MethodPost, "/asc/ssh-key", strings.NewReader(string(body)))
		if authorization != "" {
			httpReq.Header.Set("Authorization", authorization)
		}
		resp := httptest.NewRecorder()
		Routes().ServeHTTP(resp, httpReq)
		return resp.Code
	}
	assert.Equal(t, http.StatusForbidden, do(""))
	assert.Equal(t, http.StatusForbidden, do("Bearer wrong"))
	assert.Equal(t, http.StatusForbidden, do("Basic internal-secret"))
	assert.Equal(t, http.StatusOK, do("Bearer internal-secret"))
}

func TestAscSSHKeyBadRequest(t *testing.T) {
	ascAuthzSetup(t)

	content, good := ascNewSSHKey(t)
	pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(content))
	require.NoError(t, err)
	blob := pk.Marshal()
	other, _ := ascNewSSHKey(t)
	otherPK, _, _, _, err := ssh.ParseAuthorizedKey([]byte(other))
	require.NoError(t, err)

	enc := func(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
	withKey := func(typ, key, fp string) string {
		body, err := json.Marshal(private.AscSSHKeyRequest{Type: typ, Key: key, Fingerprint: fp})
		require.NoError(t, err)
		return string(body)
	}

	// Bodies that carry a key request whose fields are wrong. The authz
	// endpoint takes the same object as its ssh_key.
	badKeys := map[string]string{
		"no type":             withKey("", good.Key, ""),
		"no key":              withKey(good.Type, "", ""),
		"type too long":       withKey(strings.Repeat("a", private.AscSSHKeyMaxType+1), good.Key, ""),
		"key too long":        withKey(good.Type, strings.Repeat("A", private.AscSSHKeyMaxKey+1), ""),
		"fingerprint too big": withKey(good.Type, good.Key, strings.Repeat("a", private.AscSSHKeyMaxFingerprint+1)),
		"not base64":          withKey(good.Type, "!!!!", ""),
		"extra padding":       withKey(good.Type, good.Key+"=", ""),
		"not a key":           withKey(good.Type, enc([]byte("hello world")), ""),
		"truncated blob":      withKey(good.Type, enc(blob[:len(blob)-1]), ""),
		"trailing blob bytes": withKey(good.Type, enc(append(append([]byte{}, blob...), 0)), ""),
		"wrong type":          withKey("ssh-rsa", good.Key, ""),
		"wrong sig algo":      withKey("rsa-sha2-256", good.Key, ""),
		"wrong fingerprint":   withKey(good.Type, good.Key, ssh.FingerprintSHA256(otherPK)),
	}
	rawBodies := map[string]string{
		"empty":         ``,
		"not json":      `nope`,
		"unknown field": `{"type":"ssh-ed25519","key":"` + good.Key + `","admin":true}`,
		"trailing data": withKey(good.Type, good.Key, "") + `{}`,
		"oversize body": `{"type":"` + strings.Repeat("a", private.AscAuthzMaxBodySize) + `"}`,
	}
	for name, body := range badKeys {
		rawBodies[name] = body
	}

	for name, body := range rawBodies {
		t.Run(name, func(t *testing.T) {
			code, out := ascSSHKeyCall(t, body)
			assert.Equal(t, http.StatusBadRequest, code)
			// The reply is fixed text: it never repeats the key.
			assert.NotContains(t, out, good.Key)
			assert.NotContains(t, out, ssh.FingerprintSHA256(pk))
		})
	}
	for name, body := range badKeys {
		t.Run("authz "+name, func(t *testing.T) {
			code, out := ascAuthzCall(t, `{"owner":"user2","repo":"repo1","mode":"read","ssh_key":`+body+`}`)
			assert.Equal(t, http.StatusBadRequest, code)
			assert.NotContains(t, out, good.Key)
		})
	}

	t.Run("fingerprint is optional", func(t *testing.T) {
		code, _ := ascSSHKeyCall(t, withKey(good.Type, good.Key, ""))
		assert.Equal(t, http.StatusOK, code)
	})

	t.Run("authz mixes credentials", func(t *testing.T) {
		key, err := json.Marshal(good)
		require.NoError(t, err)
		for name, extra := range map[string]string{
			"token":  `,"token":"abc"`,
			"user":   `,"user":"user2","token":"abc"`,
			"key_id": `,"key_id":1`,
		} {
			code, _ := ascAuthzCall(t, `{"owner":"user2","repo":"repo1","mode":"read","ssh_key":`+string(key)+extra+`}`)
			assert.Equal(t, http.StatusBadRequest, code, name)
		}
	})

	t.Run("authz ssh_key must be an object", func(t *testing.T) {
		for _, body := range []string{
			`{"owner":"user2","repo":"repo1","mode":"read","ssh_key":{}}`,
			`{"owner":"user2","repo":"repo1","mode":"read","ssh_key":"abc"}`,
			`{"owner":"user2","repo":"repo1","mode":"read","ssh_key":{"type":"ssh-ed25519","key":"AAAA","extra":1}}`,
		} {
			code, _ := ascAuthzCall(t, body)
			assert.Equal(t, http.StatusBadRequest, code, body)
		}
	})
}

func TestAscSSHKeyIdentify(t *testing.T) {
	ascAuthzSetup(t)

	t.Run("unknown key", func(t *testing.T) {
		_, req := ascNewSSHKey(t)
		assert.Equal(t, private.AscSSHKeyResponse{}, ascSSHKeyDo(t, req))
	})

	t.Run("fixture user keys", func(t *testing.T) {
		// Key 1 is an RSA key of user2. The signature algorithm names of an
		// RSA key identify the same key.
		rsaContent := unittest.AssertExistsAndLoadBean(t, &asymkey_model.PublicKey{ID: 1}).Content
		req := ascSSHRequestOf(t, rsaContent)
		for _, algo := range []string{"ssh-rsa", "rsa-sha2-256", "rsa-sha2-512"} {
			req.Type = algo
			assert.Equal(t, private.AscSSHKeyResponse{Known: true, KeyID: 1, Kind: "user", UserID: 2, Username: "user2"}, ascSSHKeyDo(t, req), algo)
		}

		// Key 3 belongs to user9, who is inactive in the fixtures.
		content := unittest.AssertExistsAndLoadBean(t, &asymkey_model.PublicKey{ID: 3}).Content
		assert.Equal(t, private.AscSSHKeyResponse{}, ascSSHKeyDo(t, ascSSHRequestOf(t, content)))
	})

	t.Run("added user key", func(t *testing.T) {
		content, req := ascNewSSHKey(t)
		key := ascAddUserKey(t, 2, content)
		assert.Equal(t, private.AscSSHKeyResponse{Known: true, KeyID: key.ID, Kind: "user", UserID: 2, Username: "user2"}, ascSSHKeyDo(t, req))

		// The answer is for the exact key only.
		_, other := ascNewSSHKey(t)
		assert.Equal(t, private.AscSSHKeyResponse{}, ascSSHKeyDo(t, other))

		// Gone once the key is deleted.
		_, err := db.GetEngine(db.DefaultContext).ID(key.ID).Delete(&asymkey_model.PublicKey{})
		require.NoError(t, err)
		assert.Equal(t, private.AscSSHKeyResponse{}, ascSSHKeyDo(t, req))
	})

	t.Run("deploy key", func(t *testing.T) {
		content, req := ascNewSSHKey(t)
		dk, err := asymkey_model.AddDeployKey(db.DefaultContext, 2, "asc-dk", content, true)
		require.NoError(t, err)
		// A deploy key belongs to repositories, not to a user.
		assert.Equal(t, private.AscSSHKeyResponse{Known: true, KeyID: dk.KeyID, Kind: "deploy"}, ascSSHKeyDo(t, req))

		// A deploy key row that no repository uses grants nothing.
		_, err = db.GetEngine(db.DefaultContext).ID(dk.ID).Delete(&asymkey_model.DeployKey{})
		require.NoError(t, err)
		assert.Equal(t, private.AscSSHKeyResponse{}, ascSSHKeyDo(t, req))
	})

	t.Run("user who may not sign in", func(t *testing.T) {
		content, req := ascNewSSHKey(t)
		ascAddUserKey(t, 2, content)
		require.True(t, ascSSHKeyDo(t, req).Known)

		for name, column := range map[string]string{"inactive": "is_active", "login prohibited": "prohibit_login"} {
			t.Run(name, func(t *testing.T) {
				value := name == "login prohibited"
				_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE `user` SET "+column+" = ? WHERE id = ?", value, 2)
				require.NoError(t, err)
				defer func() {
					_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE `user` SET is_active = ?, prohibit_login = ? WHERE id = ?", true, false, 2)
					require.NoError(t, err)
				}()
				assert.Equal(t, private.AscSSHKeyResponse{}, ascSSHKeyDo(t, req))
			})
		}
	})

	t.Run("must change password", func(t *testing.T) {
		content, req := ascNewSSHKey(t)
		ascAddUserKey(t, 2, content)
		_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE `user` SET must_change_password = ? WHERE id = ?", true, 2)
		require.NoError(t, err)
		assert.Equal(t, private.AscSSHKeyResponse{}, ascSSHKeyDo(t, req))
		_, err = db.GetEngine(db.DefaultContext).Exec("UPDATE `user` SET must_change_password = ? WHERE id = ?", false, 2)
		require.NoError(t, err)
	})

	t.Run("owner is missing", func(t *testing.T) {
		content, req := ascNewSSHKey(t)
		key := ascAddUserKey(t, 2, content)
		_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE public_key SET owner_id = ? WHERE id = ?", 987654, key.ID)
		require.NoError(t, err)
		assert.Equal(t, private.AscSSHKeyResponse{}, ascSSHKeyDo(t, req))
	})
}

func TestAscSSHKeyIdentifyBrokenRows(t *testing.T) {
	ascAuthzSetup(t)

	t.Run("fingerprint of another key", func(t *testing.T) {
		contentA, reqA := ascNewSSHKey(t)
		contentB, _ := ascNewSSHKey(t)
		key := ascAddUserKey(t, 2, contentA)
		require.True(t, ascSSHKeyDo(t, reqA).Known)

		// The row names A's fingerprint but holds B's text: A is not B.
		_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE public_key SET content = ? WHERE id = ?", contentB, key.ID)
		require.NoError(t, err)
		assert.Equal(t, private.AscSSHKeyResponse{}, ascSSHKeyDo(t, reqA))
	})

	t.Run("two rows for one key", func(t *testing.T) {
		content, req := ascNewSSHKey(t)
		key := ascAddUserKey(t, 2, content)
		require.True(t, ascSSHKeyDo(t, req).Known)

		dup := *key
		dup.ID = 0
		dup.OwnerID = 9
		dup.Name = "asc-dup"
		_, err := db.GetEngine(db.DefaultContext).Insert(&dup)
		require.NoError(t, err)
		// Whose key is it? Nobody's.
		assert.Equal(t, private.AscSSHKeyResponse{}, ascSSHKeyDo(t, req))
		assert.False(t, ascSSHAuthz(t, req, "repo1", "read").Allowed)
	})

	t.Run("principal row", func(t *testing.T) {
		content, req := ascNewSSHKey(t)
		pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(content))
		require.NoError(t, err)
		_, err = db.GetEngine(db.DefaultContext).Insert(&asymkey_model.PublicKey{
			OwnerID: 2, Name: "asc-principal", Fingerprint: ssh.FingerprintSHA256(pk), Content: content, Type: asymkey_model.KeyTypePrincipal, Mode: 2,
		})
		require.NoError(t, err)
		assert.Equal(t, private.AscSSHKeyResponse{}, ascSSHKeyDo(t, req))
		assert.False(t, ascSSHAuthz(t, req, "repo1", "read").Allowed)
	})
}

func TestAscSSHKeyAuthz(t *testing.T) {
	ascAuthzSetup(t)

	// user2 owns repo1 (public) and repo2 (private); user8 has no access to repo2.
	user2Content, user2Key := ascNewSSHKey(t)
	ascAddUserKey(t, 2, user2Content)
	user8Content, user8Key := ascNewSSHKey(t)
	ascAddUserKey(t, 8, user8Content)

	t.Run("user key with read and write permission", func(t *testing.T) {
		for _, mode := range []string{"read", "write"} {
			resp := ascSSHAuthz(t, user2Key, "repo2", mode)
			assert.True(t, resp.Allowed, mode)
			assert.EqualValues(t, 2, resp.UserID)
			assert.Equal(t, "user2", resp.Username)
			assert.Zero(t, resp.DeployKeyID)
			assert.False(t, resp.IsActionsTask)
		}
		assert.Contains(t, []string{"write", "admin", "owner"}, ascSSHAuthz(t, user2Key, "repo2", "write").Perm)
	})

	t.Run("a signature algorithm of an RSA key", func(t *testing.T) {
		req := ascSSHRequestOf(t, unittest.AssertExistsAndLoadBean(t, &asymkey_model.PublicKey{ID: 1}).Content)
		req.Type = "rsa-sha2-512"
		resp := ascSSHAuthz(t, req, "repo2", "write")
		assert.True(t, resp.Allowed)
		assert.EqualValues(t, 2, resp.UserID)
	})

	t.Run("private repo without access", func(t *testing.T) {
		assert.Equal(t, private.AscAuthzResponse{}, ascSSHAuthz(t, user8Key, "repo2", "read"))
		assert.Equal(t, private.AscAuthzResponse{}, ascSSHAuthz(t, user8Key, "repo2", "write"))
		// A missing repository looks the same.
		assert.Equal(t, private.AscAuthzResponse{}, ascSSHAuthz(t, user8Key, "no-such-repo", "read"))
	})

	t.Run("public repo is read only for another user", func(t *testing.T) {
		resp := ascSSHAuthz(t, user8Key, "repo1", "read")
		assert.True(t, resp.Allowed)
		assert.EqualValues(t, 8, resp.UserID)
		assert.Equal(t, "read", resp.Perm)
		assert.False(t, ascSSHAuthz(t, user8Key, "repo1", "write").Allowed)
	})

	t.Run("unknown key", func(t *testing.T) {
		_, unknown := ascNewSSHKey(t)
		assert.Equal(t, private.AscAuthzResponse{}, ascSSHAuthz(t, unknown, "repo2", "read"))
		// An unknown key is not anonymous: it gets no read access to a public repo.
		assert.Equal(t, private.AscAuthzResponse{}, ascSSHAuthz(t, unknown, "repo1", "read"))
	})

	t.Run("read-only deploy key", func(t *testing.T) {
		content, req := ascNewSSHKey(t)
		dk, err := asymkey_model.AddDeployKey(db.DefaultContext, 2, "asc-ro", content, true)
		require.NoError(t, err)

		resp := ascSSHAuthz(t, req, "repo2", "read")
		assert.True(t, resp.Allowed)
		assert.Equal(t, dk.ID, resp.DeployKeyID)
		assert.Equal(t, "user2", resp.Username)
		assert.Equal(t, "read", resp.Perm)
		assert.False(t, ascSSHAuthz(t, req, "repo2", "write").Allowed)
	})

	t.Run("deploy key with write", func(t *testing.T) {
		content, req := ascNewSSHKey(t)
		dk, err := asymkey_model.AddDeployKey(db.DefaultContext, 2, "asc-rw", content, false)
		require.NoError(t, err)

		for _, mode := range []string{"read", "write"} {
			resp := ascSSHAuthz(t, req, "repo2", mode)
			assert.True(t, resp.Allowed, mode)
			assert.Equal(t, dk.ID, resp.DeployKeyID)
			assert.Equal(t, "write", resp.Perm, "the key's own mode, as /serv/command has it")
		}
	})

	t.Run("deploy key on another repo", func(t *testing.T) {
		content, req := ascNewSSHKey(t)
		_, err := asymkey_model.AddDeployKey(db.DefaultContext, 2, "asc-other", content, false)
		require.NoError(t, err)

		// The key belongs to repo2 only, even though repo1 is public.
		assert.Equal(t, private.AscAuthzResponse{}, ascSSHAuthz(t, req, "repo1", "read"))
		assert.Equal(t, private.AscAuthzResponse{}, ascSSHAuthz(t, req, "repo1", "write"))
	})

	t.Run("deploy key shared by two repos has its own mode in each", func(t *testing.T) {
		content, req := ascNewSSHKey(t)
		// repo1 (id 1) read only and repo2 (id 2) with write.
		_, err := asymkey_model.AddDeployKey(db.DefaultContext, 1, "asc-shared-1", content, true)
		require.NoError(t, err)
		dk2, err := asymkey_model.AddDeployKey(db.DefaultContext, 2, "asc-shared-2", content, false)
		require.NoError(t, err)

		assert.True(t, ascSSHAuthz(t, req, "repo1", "read").Allowed)
		assert.False(t, ascSSHAuthz(t, req, "repo1", "write").Allowed)
		resp := ascSSHAuthz(t, req, "repo2", "write")
		assert.True(t, resp.Allowed)
		assert.Equal(t, dk2.ID, resp.DeployKeyID)
	})

	t.Run("user who may not sign in", func(t *testing.T) {
		require.True(t, ascSSHAuthz(t, user2Key, "repo2", "read").Allowed)
		for name, column := range map[string]string{"inactive": "is_active", "login prohibited": "prohibit_login"} {
			value := name == "login prohibited"
			_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE `user` SET "+column+" = ? WHERE id = ?", value, 2)
			require.NoError(t, err)
			assert.Equal(t, private.AscAuthzResponse{}, ascSSHAuthz(t, user2Key, "repo2", "read"), name)
			// The public repo is not reachable through the key either.
			assert.Equal(t, private.AscAuthzResponse{}, ascSSHAuthz(t, user2Key, "repo1", "read"), name)
			_, err = db.GetEngine(db.DefaultContext).Exec("UPDATE `user` SET is_active = ?, prohibit_login = ? WHERE id = ?", true, false, 2)
			require.NoError(t, err)
		}
		assert.True(t, ascSSHAuthz(t, user2Key, "repo2", "read").Allowed)
	})

	t.Run("revoked key", func(t *testing.T) {
		content, req := ascNewSSHKey(t)
		key := ascAddUserKey(t, 2, content)
		require.True(t, ascSSHAuthz(t, req, "repo2", "read").Allowed)
		_, err := db.GetEngine(db.DefaultContext).ID(key.ID).Delete(&asymkey_model.PublicKey{})
		require.NoError(t, err)
		assert.Equal(t, private.AscAuthzResponse{}, ascSSHAuthz(t, req, "repo2", "read"))
	})

	t.Run("archived repo is read only", func(t *testing.T) {
		_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE repository SET is_archived = ? WHERE id = ?", true, 2)
		require.NoError(t, err)
		assert.True(t, ascSSHAuthz(t, user2Key, "repo2", "read").Allowed)
		assert.False(t, ascSSHAuthz(t, user2Key, "repo2", "write").Allowed)
	})
}

// Neither an answer nor an error repeats the key that was sent.
func TestAscSSHKeyRepliesCarryNoKey(t *testing.T) {
	ascAuthzSetup(t)

	_, req := ascNewSSHKey(t)
	body, err := json.Marshal(req)
	require.NoError(t, err)
	code, out := ascSSHKeyCall(t, string(body))
	require.Equal(t, http.StatusOK, code)
	assert.NotContains(t, out, req.Key)
	assert.NotContains(t, out, req.Fingerprint)

	code, out = ascAuthzCall(t, `{"owner":"user2","repo":"repo2","mode":"read","ssh_key":{"type":"ssh-ed25519","key":"`+req.Key+`","fingerprint":"SHA256:nope"}}`)
	require.Equal(t, http.StatusBadRequest, code)
	assert.NotContains(t, out, req.Key)
}
