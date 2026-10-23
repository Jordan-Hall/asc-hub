// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/http"

	asymkey_model "forgejo.org/models/asymkey"
	"forgejo.org/models/db"
	"forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/json"
	"forgejo.org/modules/log"
	"forgejo.org/modules/private"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"

	"golang.org/x/crypto/ssh"
)

// AscSSHKey answers `POST /api/internal/asc/ssh-key`: which Forgejo key is this
// public key, and may its owner sign in? It only reads the key tables that
// /serv/command reads. The rights on a repository are decided by AscAuthz with
// the same key. A key that is unknown, a key of a user who may not sign in and
// a deploy key that no repository uses all give the same empty answer.
func AscSSHKey(ctx *context.PrivateContext) {
	if !setting.Asc.Enabled {
		ctx.Error(http.StatusNotFound)
		return
	}

	keyReq := &private.AscSSHKeyRequest{}
	if err := decodeAscStrict(ctx, keyReq); err != nil {
		ctx.JSON(http.StatusBadRequest, private.Response{UserMsg: err.Error()})
		return
	}
	if _, _, err := parseAscSSHKey(keyReq); err != nil {
		ctx.JSON(http.StatusBadRequest, private.Response{UserMsg: err.Error()})
		return
	}

	resp, err := ascIdentifySSHKey(ctx, keyReq)
	if err != nil {
		log.Error("asc ssh-key: %v", err)
		ctx.JSON(http.StatusInternalServerError, private.Response{Err: "internal error"})
		return
	}
	ctx.JSON(http.StatusOK, resp)
}

// decodeAscStrict reads one JSON object of at most AscAuthzMaxBodySize bytes into
// v. An unknown field, a second value and a truncated body are all refused, and
// the error never carries any of the body.
func decodeAscStrict(ctx *context.PrivateContext, v any) error {
	body := http.MaxBytesReader(ctx.Resp, ctx.Req.Body, private.AscAuthzMaxBodySize)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid request body")
	}
	// Nothing but whitespace may follow the object.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid request body")
	}
	return nil
}

// parseAscSSHKey checks the fields of a key request and returns the parsed key
// with its SHA-256 fingerprint. The blob is the authority: it must be one plain
// public key with no trailing bytes, and a Type or Fingerprint that the caller
// sent must agree with it. The errors are fixed text.
func parseAscSSHKey(req *private.AscSSHKeyRequest) (ssh.PublicKey, string, error) {
	if req.Type == "" || len(req.Type) > private.AscSSHKeyMaxType {
		return nil, "", errors.New("invalid key type")
	}
	if req.Key == "" || len(req.Key) > private.AscSSHKeyMaxKey {
		return nil, "", errors.New("invalid key")
	}
	if len(req.Fingerprint) > private.AscSSHKeyMaxFingerprint {
		return nil, "", errors.New("invalid fingerprint")
	}
	blob, err := base64.StdEncoding.Strict().DecodeString(req.Key)
	if err != nil {
		return nil, "", errors.New("invalid key")
	}
	pk, err := ssh.ParsePublicKey(blob)
	if err != nil {
		return nil, "", errors.New("invalid key")
	}
	// A certificate is not a key that Forgejo stores, and ParsePublicKey may
	// tolerate bytes after the key that Marshal does not write back.
	if _, isCert := pk.(*ssh.Certificate); isCert || !bytes.Equal(pk.Marshal(), blob) {
		return nil, "", errors.New("invalid key")
	}
	if !ascSSHAlgoMatches(req.Type, pk.Type()) {
		return nil, "", errors.New("key type does not match key")
	}
	fingerprint := ssh.FingerprintSHA256(pk)
	if req.Fingerprint != "" && req.Fingerprint != fingerprint {
		return nil, "", errors.New("fingerprint does not match key")
	}
	return pk, fingerprint, nil
}

// ascSSHAlgoMatches reports whether algo names the key type, or is a signature
// algorithm of an RSA key (whose key type is always "ssh-rsa").
func ascSSHAlgoMatches(algo, keyType string) bool {
	if algo == keyType {
		return true
	}
	return keyType == ssh.KeyAlgoRSA && (algo == ssh.KeyAlgoRSASHA256 || algo == ssh.KeyAlgoRSASHA512)
}

// ascFindPublicKey finds the one user or deploy key of Forgejo that holds the
// parsed public key. It returns nil, nil when there is none. The search is by
// fingerprint, and the stored text is parsed and compared with the blob, so a
// row whose fingerprint does not describe its content never matches. Two rows
// for one key are a broken table, and then no key is returned.
func ascFindPublicKey(ctx *context.PrivateContext, pk ssh.PublicKey, fingerprint string) (*asymkey_model.PublicKey, error) {
	keys, err := db.Find[asymkey_model.PublicKey](ctx, asymkey_model.FindPublicKeyOptions{
		ListOptions: db.ListOptions{Page: 1, PageSize: 4},
		Fingerprint: fingerprint,
		KeyTypes:    []asymkey_model.KeyType{asymkey_model.KeyTypeUser, asymkey_model.KeyTypeDeploy},
	})
	if err != nil {
		return nil, err
	}

	want := pk.Marshal()
	var found *asymkey_model.PublicKey
	for _, key := range keys {
		stored, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key.Content))
		if err != nil || !bytes.Equal(stored.Marshal(), want) {
			continue
		}
		if found != nil {
			// Only IDs are logged: never key text.
			log.Warn("asc ssh-key: public keys %d and %d hold the same key", found.ID, key.ID)
			return nil, nil
		}
		found = key
	}
	return found, nil
}

func ascIdentifySSHKey(ctx *context.PrivateContext, req *private.AscSSHKeyRequest) (*private.AscSSHKeyResponse, error) {
	pk, fingerprint, err := parseAscSSHKey(req)
	if err != nil {
		return nil, err
	}
	key, err := ascFindPublicKey(ctx, pk, fingerprint)
	if err != nil || key == nil {
		return &private.AscSSHKeyResponse{}, err
	}

	if key.Type == asymkey_model.KeyTypeDeploy {
		// A deploy key is stored once and shared by every repository that uses
		// it. One that no repository uses grants nothing.
		used, err := asymkey_model.IsDeployKeyExistByKeyID(ctx, key.ID)
		if err != nil || !used {
			return &private.AscSSHKeyResponse{}, err
		}
		return &private.AscSSHKeyResponse{Known: true, KeyID: key.ID, Kind: private.AscSSHKeyKindDeploy}, nil
	}

	user, err := user_model.GetUserByID(ctx, key.OwnerID)
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			return &private.AscSSHKeyResponse{}, nil
		}
		return nil, err
	}
	if usable, err := ascUserUsable(ctx, user); err != nil || !usable {
		return &private.AscSSHKeyResponse{}, err
	}
	return &private.AscSSHKeyResponse{Known: true, KeyID: key.ID, Kind: private.AscSSHKeyKindUser, UserID: user.ID, Username: user.Name}, nil
}

// ascPrincipalFromSSHKey is the asker behind a public key that asc verified: the
// stored key is found by its content, then handled as /serv/command handles a
// key ID. An unknown key and a refused one both give no principal.
func ascPrincipalFromSSHKey(ctx *context.PrivateContext, repo *repo_model.Repository, req *private.AscSSHKeyRequest, mode perm.AccessMode) (*ascPrincipal, bool, error) {
	pk, fingerprint, err := parseAscSSHKey(req)
	if err != nil {
		return nil, false, err
	}
	key, err := ascFindPublicKey(ctx, pk, fingerprint)
	if err != nil || key == nil {
		return nil, false, err
	}
	return ascPrincipalFromPublicKey(ctx, repo, key, mode)
}
