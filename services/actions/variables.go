// Copyright 2024 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package actions

import (
	"context"
	"regexp"
	"strings"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/secret"
	"forgejo.org/modules/ascscope"
	"forgejo.org/modules/log"
	"forgejo.org/modules/util"
)

// CreateVariable adds the variable name and records doerID as the one who set it. scopes 0 gives it
// ascscope.DefaultScope.
func CreateVariable(ctx context.Context, doerID, ownerID, repoID int64, name, data string, scopes ascscope.Scope) (*actions_model.ActionVariable, error) {
	if err := secret.ValidateName(name); err != nil {
		return nil, err
	}

	if err := envNameCIRegexMatch(name); err != nil {
		return nil, err
	}

	v, err := actions_model.InsertVariable(ctx, ownerID, repoID, name, util.ReserveLineBreakForTextarea(data))
	if err != nil {
		return nil, err
	}
	if err := actions_model.SetVariableMeta(ctx, v.ID, doerID, scopes); err != nil {
		return nil, err
	}
	v.UpdaterID = doerID
	if scopes != 0 {
		v.Scopes = scopes
	}
	return v, nil
}

// UpdateVariable changes the variable variableID and records doerID as the one who set it. scopes 0 keeps the
// scopes it has.
func UpdateVariable(ctx context.Context, doerID, variableID, ownerID, repoID int64, name, data string, scopes ascscope.Scope) (bool, error) {
	if err := secret.ValidateName(name); err != nil {
		return false, err
	}

	if err := envNameCIRegexMatch(name); err != nil {
		return false, err
	}

	ok, err := actions_model.UpdateVariable(ctx, &actions_model.ActionVariable{
		ID:      variableID,
		Name:    strings.ToUpper(name),
		Data:    util.ReserveLineBreakForTextarea(data),
		OwnerID: ownerID,
		RepoID:  repoID,
	})
	if err != nil || !ok {
		return ok, err
	}
	return true, actions_model.SetVariableMeta(ctx, variableID, doerID, scopes)
}

func DeleteVariableByName(ctx context.Context, ownerID, repoID int64, name string) error {
	v, err := GetVariable(ctx, actions_model.FindVariablesOpts{
		OwnerID: ownerID,
		RepoID:  repoID,
		Name:    name,
	})
	if err != nil {
		return err
	}

	_, err = actions_model.DeleteVariable(ctx, v.ID, ownerID, repoID)
	return err
}

func GetVariable(ctx context.Context, opts actions_model.FindVariablesOpts) (*actions_model.ActionVariable, error) {
	vars, err := actions_model.FindVariables(ctx, opts)
	if err != nil {
		return nil, err
	}
	if len(vars) != 1 {
		return nil, util.NewNotExistErrorf("variable not found")
	}
	return vars[0], nil
}

// some regular expression of `variables` and `secrets`
// reference to:
// https://docs.github.com/en/actions/learn-github-actions/variables#naming-conventions-for-configuration-variables
// https://docs.github.com/en/actions/security-guides/encrypted-secrets#naming-your-secrets
// https://forgejo.org/docs/next/user/actions/basic-concepts/#name-constraints
var (
	forbiddenEnvNameCIRx = regexp.MustCompile("(?i)^CI$")
)

func envNameCIRegexMatch(name string) error {
	if forbiddenEnvNameCIRx.MatchString(name) {
		log.Error("Env Name cannot be ci")
		return util.NewInvalidArgumentErrorf("env name cannot be ci")
	}
	return nil
}
