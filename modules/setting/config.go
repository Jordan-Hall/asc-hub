// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"context"
	"strings"
	"sync"

	"forgejo.org/modules/log"
	"forgejo.org/modules/setting/config"
)

type PictureStruct struct {
	DisableGravatar       *config.Value[bool]
	EnableFederatedAvatar *config.Value[bool]
}

type OpenWithEditorApp struct {
	DisplayName string
	OpenURL     string
}

type OpenWithEditorAppsType []OpenWithEditorApp

func (t OpenWithEditorAppsType) ToTextareaString() string {
	var ret strings.Builder
	for _, app := range t {
		ret.WriteString(app.DisplayName + " = " + app.OpenURL + "\n")
	}
	return ret.String()
}

func DefaultOpenWithEditorApps() OpenWithEditorAppsType {
	return OpenWithEditorAppsType{
		{
			DisplayName: "VS Code",
			OpenURL:     "vscode://vscode.git/clone?url={url}",
		},
		{
			DisplayName: "VSCodium",
			OpenURL:     "vscodium://vscode.git/clone?url={url}",
		},
		{
			DisplayName: "Intellij IDEA",
			OpenURL:     "jetbrains://idea/checkout/git?idea.required.plugins.id=Git4Idea&checkout.repo={url}",
		},
	}
}

type RepositoryStruct struct {
	OpenWithEditorApps *config.Value[OpenWithEditorAppsType]
}

// ServiceStruct holds the sign-up switches that an administrator changes without a restart. The values are the ones of
// the [service] section until a switch is saved; ApplyRuntimeService copies the current values into Service, which the
// rest of the code reads, so the page of the switches and the sign-up page always agree.
type ServiceStruct struct {
	AllowRegistration   *config.Value[bool]
	RequireEmailConfirm *config.Value[bool]
}

type ConfigStruct struct {
	Picture    *PictureStruct
	Repository *RepositoryStruct
	Service    *ServiceStruct
}

var (
	defaultConfig     *ConfigStruct
	defaultConfigOnce sync.Once
)

func initDefaultConfig() {
	config.SetCfgSecKeyGetter(&cfgSecKeyGetter{})
	defaultConfig = &ConfigStruct{
		Picture: &PictureStruct{
			DisableGravatar:       config.ValueJSON[bool]("picture.disable_gravatar").WithFileConfig(config.CfgSecKey{Sec: "picture", Key: "DISABLE_GRAVATAR"}),
			EnableFederatedAvatar: config.ValueJSON[bool]("picture.enable_federated_avatar").WithFileConfig(config.CfgSecKey{Sec: "picture", Key: "ENABLE_FEDERATED_AVATAR"}),
		},
		Repository: &RepositoryStruct{
			OpenWithEditorApps: config.ValueJSON[OpenWithEditorAppsType]("repository.open-with.editor-apps"),
		},
		Service: &ServiceStruct{
			AllowRegistration:   config.ValueJSON[bool]("service.allow_registration").WithDefault(true),
			RequireEmailConfirm: config.ValueJSON[bool]("service.require_email_confirm"),
		},
	}
}

func Config() *ConfigStruct {
	defaultConfigOnce.Do(initDefaultConfig)
	return defaultConfig
}

type cfgSecKeyGetter struct{}

func (c cfgSecKeyGetter) GetValue(sec, key string) (v string, has bool) {
	if key == "" {
		return "", false
	}
	cfgSec, err := CfgProvider.GetSection(sec)
	if err != nil {
		log.Error("Unable to get config section: %q", sec)
		return "", false
	}
	cfgKey := ConfigSectionKey(cfgSec, key)
	if cfgKey == nil {
		return "", false
	}
	return cfgKey.Value(), true
}

// runtimeServiceFile holds what the configuration file said about the sign-up switches (taken when ApplyRuntimeService first
// runs, before any saved value is applied).
var runtimeServiceFile struct {
	once                                   sync.Once
	allowRegistration, requireEmailConfirm bool
	showRegistrationButton, openIDSignUp   bool
}

// ApplyRuntimeService makes Service follow the saved value of each sign-up switch, and the file's value where none was
// saved. It runs when the hub starts and when an administrator saves a switch. The values are plain booleans that the
// rest of the code reads without a lock; a switch is saved by one request at a time and the new value shows at the next
// request that reads it.
func ApplyRuntimeService(ctx context.Context) {
	f := &runtimeServiceFile
	f.once.Do(func() {
		f.allowRegistration, f.requireEmailConfirm = !Service.DisableRegistration, Service.RegisterEmailConfirm
		f.showRegistrationButton, f.openIDSignUp = Service.ShowRegistrationButton, Service.EnableOpenIDSignUp
		Config().Service.AllowRegistration.WithDefault(f.allowRegistration)
		Config().Service.RequireEmailConfirm.WithDefault(f.requireEmailConfirm)
	})
	cfg := Config().Service
	Service.DisableRegistration = !cfg.AllowRegistration.Value(ctx)
	// accounts cannot wait for a mail that cannot be sent
	Service.RegisterEmailConfirm = cfg.RequireEmailConfirm.Value(ctx) && MailService != nil
	// the sign-up button and the OpenID sign-up follow the switch; the file's choice stands while registration is as the file had it
	switch {
	case Service.DisableRegistration:
		Service.ShowRegistrationButton, Service.EnableOpenIDSignUp = false, false
	case f.allowRegistration:
		Service.ShowRegistrationButton, Service.EnableOpenIDSignUp = f.showRegistrationButton, f.openIDSignUp
	default:
		Service.ShowRegistrationButton, Service.EnableOpenIDSignUp = !Service.AllowOnlyExternalRegistration, Service.EnableOpenIDSignIn
	}
}
