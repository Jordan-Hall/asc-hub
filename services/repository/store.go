// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"fmt"

	"forgejo.org/models/db"
	system_model "forgejo.org/models/system"
	"forgejo.org/modules/log"
	"forgejo.org/modules/repostore"
)

// RemoveFilesWithNotice removes the storage of a repository, or of its wiki, and creates a system notice when
// that fails: the files can only be removed again by an administrator.
func RemoveFilesWithNotice(ctx context.Context, title string, ref repostore.RepoRef) {
	writer, err := repostore.Default()
	if err == nil {
		err = writer.Delete(ctx, ref)
	}
	if err != nil {
		desc := fmt.Sprintf("%s [%s]: %v", title, ref.Name, err)
		log.Warn(title+" [%s]: %v", ref.Name, err)
		// Note we use the db.DefaultContext here rather than passing in a context as the context may be cancelled
		if err = system_model.CreateNotice(db.DefaultContext, system_model.NoticeRepository, desc); err != nil {
			log.Error("CreateRepositoryNotice: %v", err)
		}
	}
}
