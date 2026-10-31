// bootstrap module must be the first one to be imported, it handles webpack lazy-loading and global errors
import './bootstrap.js';

import {initRepoActivityTopAuthorsChart} from './features/repo-activity-top-authors.ts';
import {initDashboardRepoList} from './features/dashboard-repo-list.ts';
import {initAscAgentRun} from './features/asc-agent-run.ts';

import {initGlobalCopyToClipboardListener} from './features/clipboard.js';
import {initContextPopups} from './features/contextpopup.js';
import {initRepoGraphGit} from './features/repo-graph.js';
import {initHeatmap} from './features/heatmap.js';
import {initImageDiff} from './features/imagediff.js';
import {initRepoMigration} from './features/repo-migration.js';
import {initRepoProject} from './features/repo-projects.js';
import {initTableSort} from './features/tablesort.js';
import {initAutoFocusEnd} from './features/autofocus-end.js';
import {initAdminUserListSearchForm} from './features/admin/users.js';
import {initAdminConfigs} from './features/admin/config.js';
import {initMarkupAnchors} from './markup/anchors.js';
import {initNotificationCount, initNotificationsTable} from './features/notification.js';
import {initRepoIssueContentHistory} from './features/repo-issue-content.js';
import {initStopwatch} from './features/stopwatch.js';
import {initFindFileInRepo} from './features/repo-findfile.js';
import {initCommentContent, initMarkupContent} from './markup/content.js';

import {initUserAuthOauth2, initUserAuth} from './features/user-auth.js';
import {
  initRepoIssueDue,
  initRepoIssueReferenceRepositorySearch,
  initRepoIssueTimeTracking,
  initRepoIssueWipTitle,
  initRepoPullRequestAllowMaintainerEdit,
  initRepoPullRequestReview,
  initArchivedLabelHandler,
} from './features/repo-issue.js';
import {initRepoIssueSidebarList} from './features/repo-issue-sidebar-list.ts';
import {initRepoEllipsisButton, initCommitStatuses, initCommitNotes} from './features/repo-commit.js';
import {
  initFootLanguageMenu,
  initGlobalButtonClickOnEnter,
  initGlobalButtons,
  initGlobalCommon,
  initDisabledInputs,
  initGlobalDropzone,
  initGlobalEnterQuickSubmit,
  initGlobalFormDirtyLeaveConfirm,
  initGlobalLinkActions,
} from './features/common-global.js';
import {initRepoTopicBar} from './features/repo-home.js';
import {initAdminEmails} from './features/admin/emails.js';
import {initAdminCommon} from './features/admin/common.js';
import {initRepoTemplateSearch} from './features/repo-template.js';
import {initRepoCodeView} from './features/repo-code.ts';
import {initSshKeyFormParser} from './features/sshkey-helper.js';
import {initRepoArchiveLinks} from './features/repo-common.js';
import {initRepoMigrationStatusChecker} from './features/repo-migrate.js';
import {initAscByoMigration} from './features/asc-byo.js';
import {initAscUi} from './features/asc-ui.js';
import {initDsShell} from './features/ds-shell.js';
import {initDsToast} from './features/ds-toast.js';
import {initLivePreview, initImagePreview, initLayoutToggle, initPasswordStrength} from './features/ds-forms.js';
import {initPrefs} from './features/ds-prefs.js';
import {initBulk} from './features/ds-bulk.js';
import {initAscKeys, initAscInboxBadge} from './features/asc-keys.js';
import {initAscPush} from './features/asc-push.js';
import {initAscSignup} from './features/asc-signup.js';
import {initAscPassword} from './features/asc-password.js';
import {initAscFeedFilter} from './features/asc-feed-filter.js';
import {initAscScrollFocus} from './features/asc-scroll-focus.js';
import {initAscReview} from './features/asc-review.js';
import {initAscDevice} from './features/asc-device.js';
import {initAscDocsToc} from './features/asc-docs-toc.js';
import {initAscDrafts} from './features/asc-drafts.js';
import {initAscDocs} from './features/asc-docs.js';
import {initAscCodeBoxes} from './features/asc-codeboxes.js';
import {initAscOffline} from './features/asc-offline.js';
import {initAscRepoKeys} from './features/asc-repo-keys.js';
import {initAscDensity} from './features/asc-density.js';
import {initAscBoard} from './features/asc-board.js';
import {initRepoNew} from './features/repo-new.js';
import {initUsernameCheck} from './features/name-check.js';
import {initAscConfirm} from './features/asc-confirm.js';
import {initLocalTime} from './features/local-time.js';
import {initAscSecurity} from './features/asc-security.js';
import {initAscOps} from './features/asc-ops.js';
import {initAscMeters} from './features/asc-meters.js';
import {initAscIntent} from './features/asc-intent.js';
import {initAscTitleCounter} from './features/asc-title-counter.js';
import {initAscDraft} from './features/asc-draft.js';
import {initAscNewChangeDraft} from './features/asc-newchange-draft.js';
import {initAscResponsiveDetails} from './features/asc-responsive-details.js';
import {
  initRepoSettingGitHook,
  initRepoSettingsCollaboration,
  initRepoSettingsSaveBar,
  initRepoSettingsTopics,
  initVisibilityConfirm,
  initRepoSettingSearchTeamBox,
} from './features/repo-settings.js';
import {initRepoDiffView} from './features/repo-diff.js';
import {initOrgTeamSearchRepoBox} from './features/org-team.js';
import {initUserAuthWebAuthn, initUserAuthWebAuthnRegister} from './features/user-auth-webauthn.js';
import {initRepoRelease, initRepoReleaseNew} from './features/repo-release.js';
import {initRepoEditor} from './features/repo-editor.js';
import {initAddOrgMemberButton} from './features/add-org-member.ts';
import {initCompSearchUserBox} from './features/comp/SearchUserBox.js';
import {initInstall} from './features/install.js';
import {initCompWebHookEditor} from './features/comp/WebHookEditor.js';
import {initRepoBranchButton} from './features/repo-branch.ts';
import {initCommonOrganization} from './features/common-organization.js';
import {initRepoWikiForm} from './features/repo-wiki.js';
import {initRepoCommentForm, initRepository} from './features/repo-legacy.js';
import {initCopyContent} from './features/copycontent.js';
import {initCaptcha} from './features/captcha.js';
import {initRepositoryActionView} from './features/repo-action-view.ts';
import {initGlobalTooltips} from './modules/tippy.js';
import {initDropdowns} from './modules/dropdown.ts';
import {initGiteaFomantic} from './modules/fomantic.js';
import {onDomReady} from './utils/dom.js';
import {initRepoIssueList} from './features/repo-issue-list.js';
import {initCommonIssueListQuickGoto} from './features/common-issue-list.js';
import {initRepoContributors} from './features/contributors.js';
import {initRepoCodeFrequency} from './features/code-frequency.js';
import {initRepoRecentCommits} from './features/recent-commits.js';
import {initRepoDiffCommitBranchesAndTags} from './features/repo-diff-commit.js';
import {initDirAuto} from './modules/dirauto.js';
import {initRepositorySearch} from './features/repo-search.js';
import {initColorPickers} from './features/colorpicker.js';
import {initRepoMilestoneEditor} from './features/repo-milestone.js';
import {initModalClose} from './modules/modal.ts';
import {initAuthorizedIntegrationClaimRuleEditor} from './features/authorized-integration.js';

// Init Gitea's Fomantic settings
initGiteaFomantic();
initDirAuto();

onDomReady(() => {
  initGlobalCommon();

  initDisabledInputs(document);
  initGlobalTooltips();
  initGlobalButtonClickOnEnter();
  initGlobalButtons();
  initGlobalCopyToClipboardListener();
  initGlobalDropzone();
  initGlobalEnterQuickSubmit();
  initGlobalFormDirtyLeaveConfirm();
  initGlobalLinkActions();
  initDropdowns();

  initCommonOrganization();
  initCommonIssueListQuickGoto();

  initCompSearchUserBox();
  initCompWebHookEditor();

  initInstall();

  initFootLanguageMenu();

  initCommentContent();
  initContextPopups();
  initHeatmap();
  initImageDiff();
  initMarkupAnchors();
  initMarkupContent();
  initSshKeyFormParser();
  initStopwatch();
  initTableSort();
  initAutoFocusEnd();
  initFindFileInRepo();
  initCopyContent();

  initAdminCommon();
  initAdminEmails();
  initAdminUserListSearchForm();
  initAdminConfigs();

  initDashboardRepoList();
  initAscAgentRun();

  initNotificationCount();
  initNotificationsTable();

  initOrgTeamSearchRepoBox();

  initRepoActivityTopAuthorsChart();
  initRepoArchiveLinks();
  initRepoBranchButton();
  initRepoCodeView();
  initRepoCommentForm();
  initRepoEllipsisButton();
  initRepoDiffCommitBranchesAndTags();
  initRepoEditor();
  initAddOrgMemberButton();
  initRepoGraphGit();
  initRepoIssueContentHistory();
  initRepoIssueDue();
  initRepoIssueList();
  initRepoIssueSidebarList();
  initArchivedLabelHandler();
  initRepoIssueReferenceRepositorySearch();
  initRepoIssueTimeTracking();
  initRepoIssueWipTitle();
  initRepoMigration();
  initRepoMigrationStatusChecker();
  initAscByoMigration();
  initAscUi();
  initDsShell();
  initDsToast();
  initLivePreview();
  initImagePreview();
  initLayoutToggle();
  initPasswordStrength();
  initPrefs();
  initBulk();
  initAscKeys();
  initAscPush();
  initAscSignup();
  initAscPassword();
  initAscFeedFilter();
  initAscScrollFocus();
  initAscReview();
  initAscDevice();
  initAscDocsToc();
  initAscDrafts();
  initAscDocs();
  initAscCodeBoxes();
  initAscOffline();
  initAscRepoKeys();
  initAscDensity();
  initAscBoard();
  initRepoNew();
  initUsernameCheck();
  initAscConfirm();
  initLocalTime();
  initAscSecurity();
  initAscOps();
  initAscMeters();
  initAscIntent();
  initAscTitleCounter();
  initAscDraft();
  initAscNewChangeDraft();
  initAscResponsiveDetails();
  initAscInboxBadge();
  initRepoProject();
  initRepoPullRequestAllowMaintainerEdit();
  initRepoPullRequestReview();
  initRepoRelease();
  initRepoReleaseNew();
  initRepoSettingGitHook();
  initRepoSettingSearchTeamBox();
  initRepoSettingsCollaboration();
  initRepoSettingsSaveBar();
  initRepoSettingsTopics();
  initVisibilityConfirm();
  initRepoTemplateSearch();
  initRepoTopicBar();
  initRepoWikiForm();
  initRepository();
  initRepositoryActionView();
  initRepositorySearch();
  initRepoContributors();
  initRepoCodeFrequency();
  initRepoRecentCommits();
  initRepoMilestoneEditor();

  initCommitStatuses();
  initCommitNotes();
  initCaptcha();

  initUserAuthOauth2();
  initUserAuthWebAuthn();
  initUserAuthWebAuthnRegister();
  initUserAuth();
  initRepoDiffView();
  initColorPickers();
  initModalClose();
  initAuthorizedIntegrationClaimRuleEditor();

  // Deactivate CSS-only noJS usability supplements
  document.body.classList.remove('no-js');
});
