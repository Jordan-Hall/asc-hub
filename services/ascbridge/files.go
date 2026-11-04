// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbridge

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// ReadFileByID reads at most limit bytes of the file asc stores under id (hex, 32 bytes) in repo (owner/name),
// with the internal token. It is for content the hub already holds an id of from an answer of asc, such as the
// sides of a conflict. truncated is true when the file is longer than limit. An error never carries the
// server's message.
func ReadFileByID(ctx context.Context, repo, id string, limit int) (data []byte, truncated bool, err error) {
	raw, err := hexID(id)
	if err != nil || limit <= 0 {
		return nil, false, fmt.Errorf("%w: not a file id", ErrStackBadRequest)
	}
	api, err := repoAPI()
	if err != nil {
		return nil, false, err
	}
	ctx, cancel := context.WithTimeout(ascclient.WithSystem(ctx), CallTimeout)
	defer cancel()
	// One byte more than the limit says whether the file is longer.
	st, err := api.ReadFile(ctx, &ascv1.ReadFileRequest{
		Repo:   &ascv1.RepoRef{Name: repo},
		At:     &ascv1.ReadFileRequest_FileId{FileId: raw},
		Length: uint64(limit) + 1,
	})
	if err != nil {
		return nil, false, mapStackError("ReadFile", err)
	}
	var total uint64
	for first := true; ; first = false {
		m, err := st.Recv()
		if errors.Is(err, io.EOF) {
			if first {
				return nil, false, fmt.Errorf("%w: ReadFile sent nothing", ErrBadAnswer)
			}
			break
		}
		if err != nil {
			return nil, false, mapStackError("ReadFile", err)
		}
		if first {
			total = m.GetTotalSize()
		}
		if len(data)+len(m.GetData()) > limit+1 {
			return nil, false, fmt.Errorf("%w: ReadFile sent more than was asked", ErrBadAnswer)
		}
		data = append(data, m.GetData()...)
	}
	if uint64(len(data)) > total {
		return nil, false, fmt.Errorf("%w: ReadFile sent more than the file", ErrBadAnswer)
	}
	if len(data) > limit {
		return data[:limit], true, nil
	}
	if uint64(len(data)) < total {
		return data, true, nil
	}
	return data, false, nil
}

func hexID(id string) ([]byte, error) {
	raw, err := hex.DecodeString(id)
	if err != nil || len(raw) != idLen {
		return nil, errors.New("not an id")
	}
	return raw, nil
}
