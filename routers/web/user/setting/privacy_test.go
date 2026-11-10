// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/structs"
	"forgejo.org/services/contexttest"
	user_service "forgejo.org/services/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockAs submits the block-by-username form of the Privacy page as user 2 and returns the answer a person sees.
func blockAs(t *testing.T, name string) (status int, location, flash string) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "POST /user/settings/privacy/block")
	contexttest.LoadUser(t, ctx, 2)
	ctx.Req.Form = url.Values{"username": {name}}
	PrivacyBlock(ctx)
	return resp.Code, resp.Header().Get("Location"), ctx.Flash.ErrorMsg
}

func TestPrivacyBlockDoesNotRevealHiddenAccounts(t *testing.T) {
	unittest.PrepareTestEnv(t)

	var hidden []*user_model.User
	require.NoError(t, db.GetEngine(t.Context()).Where("visibility = ? AND type = ? AND id != 2", structs.VisibleTypePrivate, user_model.UserTypeIndividual).Find(&hidden))
	require.NotEmpty(t, hidden, "the fixtures have a private account")
	var org user_model.User
	has, err := db.GetEngine(t.Context()).Where("type = ?", user_model.UserTypeOrganization).Get(&org)
	require.NoError(t, err)
	require.True(t, has)

	missingStatus, missingLocation, missingFlash := blockAs(t, "no_such_person_zz")
	assert.NotEmpty(t, missingFlash)

	// a private account, an organization and a name that does not exist all get the same answer (with the typed name)
	for _, name := range []string{hidden[0].Name, org.Name} {
		status, location, flash := blockAs(t, name)
		assert.Equal(t, missingStatus, status, name)
		assert.Equal(t, missingLocation, location, name)
		assert.Equal(t, strings.ReplaceAll(missingFlash, "no_such_person_zz", name), flash, name)
	}
	require.NoError(t, err)
	assert.False(t, user_model.IsBlocked(t.Context(), 2, hidden[0].ID), "nothing was blocked")
}

func TestUndoUnblockOfferLastsTenSecondsAndNamesAVisibleUnblockedPerson(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	now := time.Unix(1_700_000_000, 0)
	offer := func(query string, at time.Time) string {
		ctx, _ := contexttest.MockContext(t, "user/settings/privacy?"+query)
		contexttest.LoadUser(t, ctx, 2)
		ctx.IsSigned = true
		return undoUnblockOffer(ctx, at)
	}
	q := func(name string, t0 time.Time) string {
		return url.Values{"undo": {name}, "t": {strconv.FormatInt(t0.Unix(), 10)}}.Encode()
	}
	assert.Equal(t, "user4", offer(q("user4", now), now.Add(3*time.Second)))
	assert.Empty(t, offer(q("user4", now), now.Add(11*time.Second)), "after ten seconds there is no offer")
	assert.Empty(t, offer(q("user4", now), now.Add(-time.Second)), "an address from the future offers nothing")
	assert.Empty(t, offer(q("user2", now), now), "not oneself")
	assert.Empty(t, offer(q("no-such-person", now), now))
	assert.Empty(t, offer("undo=user4&t=x", now))
	assert.Empty(t, offer("", now))

	// a person who is blocked now needs no undo
	require.NoError(t, user_service.BlockUser(db.DefaultContext, 2, 4))
	assert.Empty(t, offer(q("user4", now), now))
}

func TestUndoSwitchOfferLastsTenSecondsAndSendsTheOppositeOfWhatIsStored(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	now := time.Unix(1_700_000_000, 0)
	offer := func(query string, at time.Time, current map[string]bool) *SwitchUndo {
		ctx, _ := contexttest.MockContext(t, "user/settings/privacy?"+query)
		contexttest.LoadUser(t, ctx, 2)
		return undoSwitchOffer(ctx, at, current)
	}
	q := func(name string, t0 time.Time) string {
		return url.Values{"switched": {name}, "t": {strconv.FormatInt(t0.Unix(), 10)}}.Encode()
	}
	got := offer(q("show_online", now), now.Add(2*time.Second), map[string]bool{"show_online": true})
	require.NotNil(t, got)
	assert.Equal(t, SwitchUndo{Setting: "show_online", Label: "asc_work.sp_show_online", On: true}, *got)
	got = offer(q("hide_email", now), now, map[string]bool{"hide_email": false})
	require.NotNil(t, got)
	assert.False(t, got.On)
	assert.Nil(t, offer(q("show_online", now), now.Add(11*time.Second), nil), "after ten seconds there is no offer")
	assert.Nil(t, offer(q("show_online", now), now.Add(-time.Second), nil), "an address from the future offers nothing")
	assert.Nil(t, offer(q("is_admin", now), now, nil), "only the switches of the page")
	assert.Nil(t, offer("switched=show_online&t=x", now, nil))
	assert.Nil(t, offer("", now, nil))
	assert.Equal(t, "/user/settings/privacy?switched=show_online&t=1700000000", switchedLink("show_online", now))
}
