// Copyright 2015 The Gogs Authors. All rights reserved.
// Copyright 2019 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package git

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"

	"forgejo.org/modules/log"
	"forgejo.org/modules/typesniffer"
)

// Blob represents a Git object.
type Blob struct {
	ID ObjectID

	gotSize bool
	size    int64
	name    string
	repo    *Repository
}

// Size returns the uncompressed size of the blob
func (b *Blob) Size() int64 {
	if b.gotSize {
		return b.size
	}

	be, err := b.repo.Backend()
	if err != nil {
		log.Debug("error whilst reading size for %s in %s. Error: %v", b.ID.String(), b.repo.Path, err)
		return 0
	}
	infos, err := be.StatObjects(b.repo.Ctx, []ObjectID{b.ID})
	if err != nil || len(infos) != 1 || infos[0].Missing {
		log.Debug("error whilst reading size for %s in %s. Error: %v", b.ID.String(), b.repo.Path, err)
		return 0
	}
	b.size = infos[0].Size
	b.gotSize = true

	return b.size
}

// open opens the blob content, truncated to limit bytes when limit is not negative.
func (b *Blob) open(limit int64) (io.ReadCloser, error) {
	be, err := b.repo.Backend()
	if err != nil {
		return nil, err
	}
	rc, size, err := be.OpenBlob(b.repo.Ctx, b.ID, limit)
	if err != nil {
		return nil, err
	}
	b.gotSize = true
	b.size = size
	return rc, nil
}

// DataAsync gets a ReadCloser for the contents of a blob without reading it all.
// Calling the Close function on the result will discard all unread output.
func (b *Blob) DataAsync() (io.ReadCloser, error) {
	return b.open(-1)
}

// Name returns name of the tree entry this blob object was created from (or empty string)
func (b *Blob) Name() string {
	return b.name
}

// NewReader return a blob-reader which fails immediately with [BlobTooLargeError] if the file is bigger than the limit
func (b *Blob) NewReader(limit int64) (rc io.ReadCloser, actualSize int64, err error) {
	actualSize = b.Size()
	if actualSize > limit {
		return nil, actualSize, BlobTooLargeError{
			Size:  actualSize,
			Limit: limit,
		}
	}
	rc, err = b.open(-1)
	if err != nil {
		return nil, actualSize, err
	}
	return rc, actualSize, nil
}

// NewTruncatedReader return a blob-reader which silently truncates when the limit is reached (io.EOF will be returned)
func (b *Blob) NewTruncatedReader(limit int64) (rc io.ReadCloser, fullSize int64, err error) {
	rc, err = b.open(max(limit, 0))
	if err != nil {
		return nil, 0, err
	}
	return rc, b.size, nil
}

type BlobTooLargeError struct {
	Size, Limit int64
}

func (b BlobTooLargeError) Error() string {
	return fmt.Sprintf("blob: content larger than limit (%d > %d)", b.Size, b.Limit)
}

// GetContentBase64 Reads the content of the blob and returns it as base64 encoded string.
// Returns [BlobTooLargeError] if the (unencoded) content is larger than the limit.
func (b *Blob) GetContentBase64(limit int64) (string, error) {
	rc, size, err := b.NewReader(limit)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	encoding := base64.StdEncoding
	buf := bytes.NewBuffer(make([]byte, 0, encoding.EncodedLen(int(size))))

	encoder := base64.NewEncoder(encoding, buf)

	if _, err := io.Copy(encoder, rc); err != nil {
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// GuessContentType guesses the content type of the blob.
func (b *Blob) GuessContentType() (typesniffer.SniffedType, error) {
	r, err := b.DataAsync()
	if err != nil {
		return typesniffer.SniffedType{}, err
	}
	defer r.Close()

	return typesniffer.DetectContentTypeFromReader(r, b.Name())
}

// GetBlob finds the blob object in the repository.
func (repo *Repository) GetBlob(idStr string) (*Blob, error) {
	id, err := NewIDFromString(idStr)
	if err != nil {
		return nil, err
	}
	if id.IsZero() {
		return nil, ErrNotExist{id.String(), ""}
	}
	return &Blob{
		ID:   id,
		repo: repo,
	}, nil
}
