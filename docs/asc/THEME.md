# ASC design system (ASC Hub UI, v3)

The hub UI is one stylesheet family, `web_src/css/asc-ds/` (`tokens`, `base`, `layout`, `components`, `landing`, `utilities`, imported by `index.css`), and a small set of Go template partials in `templates/shared/ds/`. Every page group builds from these. A page never styles itself: it composes the classes and partials below. The boards are `project-v3/*.dc.html` (A1 `components` is the system board, A2 `Shell` the chrome); the rules come from `DESIGN-V3.md`.

A living gallery of everything here renders at `/-/demo/ds` when `RUN_MODE` is not `prod`. Screenshot it at 390 and 1440, light and dark, after a CSS change.

## Rules a page group follows

1. **Mobile first.** The base rules are for 320 to 390 px; `min-width` queries add columns at 640, 768, 1024. No horizontal page scroll from 320 (code, tables and diffs scroll inside their own box). Targets are 44 px (`--touch`) on a phone, one primary action per screen, filters are a horizontally scrolling pill row, row actions drop below the row, menus are sheets and the account, create and search overlays are bottom sheets or full screen under 640.
2. **Tokens, never hex.** Colour comes from the semantic variables (`var(--surface)`, `var(--line)`, `var(--text-2)`, `var(--link)`, `var(--fill)`, `var(--ok)`, `var(--warn)`, `var(--err)`, `var(--agent)`, ...). Light is the default, dark is `:root[data-theme*="dark"]` and the system query; one token set serves both. The navy header, terminal and diff colours are fixed.
3. **No inline style, no hardcoded English.** Every string is `ctx.Locale.Tr "section.key"` in `options/locale/locale_en-US.ini` (the shell's keys live in `[asc_shell]`, the account pages in `[asc_auth]`, the landing page in `[startpage]`). The CSP forbids inline styles; use a class.
4. **Words, not colour.** A status chip always carries text; state also sets `aria-current`, `aria-selected`, `aria-pressed` or `aria-invalid`. Icons next to text are `aria-hidden`. Focus rings are never removed.
5. **One navigation model.** Views of one object are `.ds-tabs`; areas with many pages (settings, admin) use the settings shell. Never put a second tab strip over a page the left menu already reaches.
6. **Keep the hooks.** Fomantic and Gitea hooks keep working inside DS pages: `show-modal`, `delete-button`, `link-action`, `data-clipboard-text`, `.ui.dropdown`, `data-tab`, CSRF forms (`{{.CsrfTokenHtml}}`). A DS class never replaces a hook class; add the DS class next to it.

## Tokens (`tokens.css`)

Surfaces `--bg --surface --surface-2 --surface-3 --canvas --hover`; lines `--line --line-2 --line-3 --line-strong`; text `--text --text-1 --text-b --text-2 --text-3` (2 and 3 are the 4.5:1 secondary and meta text); `--link --fill --fill-hover --on-fill --primary-soft --primary-tint --primary-line --accent --focus`; status `--ok --warn --err --agent` each with `-soft` and `-line`; diff `--add-* --del-* --note-*`; inverse `--inv-bg --inv-text`; fixed navy set `--navy --navy-line --navy-field --navy-text --navy-text-2 --navy-text-3 --navy-hover`; type `--font-sans --font-mono`; shape `--r-btn 8 --r-row 10 --r-card 12 --r-dialog`; size `--header-h 56` (60 from 1024) `--bnav-h 56 --menu-w 250 --touch 44 --input-h --gutter 16` (24 from 1024) `--container 1200`; layers `--z-sticky --z-header --z-bnav --z-pop --z-scrim --z-sheet --z-toast`; `--row-pad-y` follows the density switch (`data-density="comfy|compact"`).

Colour mode: `data-theme` on `<html>` is `asc-light`, `asc-dark` or `asc-auto` and is set before first paint by `templates/base/head_style.tmpl` from the saved choice, the account theme, then the system. Buttons with `data-ds-theme-set="light|dark|system"` and `data-ds-density-set="comfy|compact"` change it; `base/theme_toggle` draws the three-button control.

## Type (`base.css`)

`.ds-t-display` (hero, 38/52/68), `.ds-t-h1` (page title 30/36/42), `.ds-t-h2`, `.ds-t-h3`, `.ds-t-h4`, `.ds-t-lead`, `.ds-t-body`, `.ds-t-small`, `.ds-t-micro` (12 px caps group label), `.ds-t-eyebrow` (13 px caps teal kicker). Colour helpers `.ds-c-2 .ds-c-3 .ds-c-link .ds-c-ok .ds-c-warn .ds-c-err .ds-c-agent`; weight `.ds-b .ds-sb`; `.ds-mono`, `.ds-num` (tabular figures); links `.ds-link`, `.ds-prose a`. Nothing under 12 px.

## Layout (`layout.css`, `utilities.css`)

* Containers: `.ds-container` (1200, gutters 16/24), `-narrow`, `-wide`, `-xl`, `-form`; `.ds-band` (full-bleed band with a container inside), `.ds-section` (vertical rhythm 40/64/88).
* Grids: `.ds-grid` (+ `.ds-grid-gap-20|24`), `.ds-grid-stats` (2 columns on a phone, 3 to 5 wider), `.ds-split` (content + `.ds-rail` right rail from 1024), `.ds-row`, `.ds-row-wrap`, `.ds-stack` (`-8 -12 -16 -24`), `.ds-gap-4|8|12|16|24`, `.ds-grow`, `.ds-min0`, `.ds-ml-auto`.
* Utilities: `.ds-hidden`, `.ds-only-mobile`, `.ds-only-desktop`, `.ds-hide-xs|sm|md`, `.ds-show-sm-only`, `.ds-truncate`, `.ds-break`, `.ds-text-right|center`, spacing `.ds-mt-0|8|12|16|24`, `.ds-mb-0|8|16|24`, `.ds-w-full`, `.ds-flex`, `.ds-inline-flex`, `.ds-items-center`, `.ds-justify-between`. The `hidden` attribute always wins over a component's `display`.

## The shell (`templates/base/*`)

* `base/head` opens `<body class="ds-app">` > `.ds-shell`: skip link, `.ds-hdr` (desktop bar from 1024: brand, nav, search that opens the palette, Inbox, notifications, Create, account; phone and tablet: `.ds-topbar` with menu, brand, search, account), then the overlays (`.ds-scrim`, `.ds-drawer`, `.ds-pop` account and create menus, `.ds-palette`) and for a signed-in person the phone bottom navigation `.ds-bnav` (Inbox, Changes, Explore, Search, Me; the active entry carries `aria-current`). Overlays open without script through `:target`; `web_src/js/features/ds-shell.js` upgrades them (focus trap, Esc, scrim, palette filter, `/` and Ctrl K, footer accordions, language menu). Triggers are elements with `data-ds-open="id"`; closers have `data-ds-close`.
* `base/footer` closes the shell with `.ds-footer` (four link groups and the theme switch for visitors, one compact line for a signed-in person and on sign in and sign up).
* Flash messages: `base/alert` draws `.ds-flash` / `.ds-banner`.
* Page wrapper: `<div role="main" aria-label="{{.Title}}" class="ds-main …">`. Legacy pages keep `.page-content` and still work; `.ds-shell > .page-content` grows to fill.

### Top bar of a page one level down: `shared/ds/subbar`

`{{template "shared/ds/subbar" (dict "Data" . "Title" "Settings" "BackHref" "…")}}` as the first child of the main element. Under 1024 it replaces the brand bar with a back arrow (or the menu button without `BackHref`), the title, search and account; from 1024 it is hidden and the header carries the page.

## Components (`components.css`)

| Component | Classes (contract) |
| --- | --- |
| Button | `.ds-btn` + `-primary -secondary -ghost -danger -danger-fill -accent` (on navy only) `-ondark -ondark-text -link`; size `-sm -lg`; `-block`; `-icon` (needs `aria-label`). Min height 44 on a phone. |
| Field | `.ds-field` > `.ds-label` (+ `.ds-opt`), `.ds-input|.ds-select|.ds-textarea` (`.is-err` or `aria-invalid="true"` for an error), `.ds-hint`, `.ds-err-msg`; `.ds-inwrap` puts an icon or a button inside the input; `.ds-form` stacks fields (16 px), `.ds-form-row.ds-form-row-2` two columns from 640; `.ds-fieldset`. |
| Choice | `.ds-check` (`<input type="checkbox|radio">` + `<label>`), `.ds-switch` (`<label class="ds-switch"><input type="checkbox" role="switch"><span class="ds-switch-track"></span><span>Text</span></label>`), `.ds-seg` (segmented control, buttons with `aria-pressed`), `.ds-chip-radio`, `.ds-selectpill` (a select shown as "Sort: Newest"). |
| Chip, count, dot | `.ds-chip` + `-ok -warn -err -info -agent -outline -lg`; `.ds-count` (+ `-hot` amber, `-agent`); `.ds-dot` + `-ok -warn -err -info -agent`; `.ds-chip-btn` filter pill (`aria-current="true"` when on). |
| Avatar | `.ds-av` + `-24 -32 -40 -48 -56 -72 -96`, `-agent` (violet, never a photo), `-org` (rounded square); `.ds-av-stack`. Hub avatars (`ctx.AvatarUtils.Avatar`) are styled as `.ds-av`. |
| Card | `.ds-card` > `.ds-card-h` (title row), `.ds-card-b`, `.ds-card-f`; `.ds-card-inset`; `.ds-card-link` (whole card is a link); `.ds-callout` (info ground); `.ds-note`, `.ds-intent`. |
| List | `.ds-list` > `.ds-li` > `.ds-li-main` (`.ds-li-title` with `.ds-li-owner`, `.ds-li-sub`, `.ds-li-meta`), `.ds-li-stat`, `.ds-li-actions` (below the row on a phone, beside it from 768), `.ds-li-link` (the row is the link). Use `shared/ds/list_row` for the common row. |
| Table | `.ds-table-wrap` > `.ds-table` (`th` group-label heads, `.ds-r` right aligned numbers). With `data-label` on every `td` the table becomes a stack of cards under 640. |
| Tabs | `<nav class="ds-tabs" aria-label="…">` > `shared/ds/tab` (`.ds-tab`, `aria-current="page"` or `aria-selected`); scrolls sideways on a phone. `.ds-crumbs` for a breadcrumb; `.ds-sidenav` for a plain side list. |
| Pager | `base/paginate` renders `.ds-pager-wrap` > `.ds-pager` (previous, numbers, next; "Page 2 of 9" from 768). |
| Filter bar | `.ds-filterbar` > `.ds-filterbar-row` > `.ds-filterbar-form` (search `.ds-inwrap`), `.ds-scrollx` pill row, `.ds-sortlink`. `shared/ds/filter_bar` for the search row. |
| Banner, toast | `.ds-banner` + `-ok -warn -err` (`role="status"` or `alert`), `.ds-toast` in `.ds-toasts` (inverse surface, with Undo). |
| Dialog, sheet | native `<dialog id="x"><article><header>Title</header><div class="content">…</div><div class="actions">…</div></article></dialog>` (centred dialog from 640, bottom sheet under); open with `class="show-modal" data-modal="#x"`; `.cancel` closes. `shared/ds/confirm`. |
| Empty, loading, error | `.ds-empty` (+ `-err`), `.ds-sk` skeleton, `.ds-bar` progress. `shared/ds/empty`. |
| Stat | `.ds-stat` > `.ds-stat-label`, `.ds-stat-value`, `.ds-stat-sub` in a `.ds-grid-stats`. `shared/ds/stat`. |
| Command and terminal | `.ds-cmd` (+ `-scroll`; give the `<code>` `tabindex="0"`) with `.ds-pr` prompt and `.ds-copy` (`data-clipboard-text`); `.ds-term` > `.ds-term-hd`, `.ds-term-bd` (fixed navy). |
| Code and diff | `.ds-codebox` > `.ds-codebox-hd`, `.ds-codebox-bd` (scrolls inside); `.ds-diff` with rows `.ds-l` (`.ds-a` added, `.ds-d` removed, `.ds-n` gutter number). Give a scroll area `tabindex="0"`, `role="region"` and a name. `shared/ds/code_box`. |
| Misc | `.ds-kbd`, `.ds-tip`, `.ds-acc` (accordion `<details>`), `.ds-details`, `.ds-or` ("or continue with"), `.ds-sticky-actions` (one primary action sticks above the bottom nav on a phone, static from 1024). |

## Partials (`templates/shared/ds/*`, `base/paginate`)

All take a `dict`. Optional props may be omitted.

| Partial | Props |
| --- | --- |
| `shared/ds/page_head` | `Title` (the page's h1), `Prefix`, `Description`, `PrimaryLabel` + `PrimaryHref`, `SecondaryLabel` + `SecondaryHref`, `HideOnMobile` (the title is read but the sub bar draws it under 1024) |
| `shared/ds/subbar` | `Data` (`.`), `Title`, `BackHref`, `BackLabel` |
| `shared/ds/tab` | `Label`, `Href`, `Active`, `Count`, `ShortLabel` (phone text) |
| `shared/ds/pill` | `Label`, `Href`, `Active`, `Count` (one filter pill of a `.ds-scrollx` row) |
| `shared/ds/list_row` | `Title`, `Href`, `Owner`, `Chip` + `ChipTone` (`ok warn err info agent`), `Sub`, `Meta1`..`Meta3`, `StatValue` + `StatLabel`, `Avatar` (HTML). Renders an `<li class="ds-li">` for a `.ds-list` |
| `shared/ds/stat` | `Label`, `Value`, `Sub`, `Href` |
| `shared/ds/empty` | `Icon` (a `dsi-` name, default `book`), `Title`, `Body`, `Tone` (`err`), `PrimaryLabel` + `PrimaryHref`, `SecondaryLabel` + `SecondaryHref` |
| `shared/ds/field` | `ID`, `Name`, `Label`; `Type`, `Value`, `Placeholder`, `Autocomplete`, `InputMode`, `Pattern`, `MaxLength`; flags `Required Optional Autofocus Disabled Readonly Mono Reveal Error`; `Hint`, `ErrorText`, `Extra` (`off` turns autocorrect and capitalise off), `LabelHref` + `LabelLink` |
| `shared/ds/filter_bar` | `Label` (accessible name), `Name` (default `q`), `Value`, `Placeholder`, `Action`, `Hidden` (dict of kept filters), `Wide` |
| `shared/ds/code_box` | `Title`, `Meta`, `Body` (HTML), `Kind` (`diff`) |
| `shared/ds/confirm` | `Data` (`.`, for CSRF), `ID`, `Title`, `Body`, `Action`, `Method`, `Confirm` (button label), `Danger`, `Fields` (hidden inputs) |
| `base/paginate` | reads `.Page` and `.Link` as before; renders the DS pager |
| `shared/ds/snav_bar`, `snav_who`, `snav_item` | the pieces of a settings menu, below |
| `base/theme_toggle` | `Data`, `Auto` |

Icons are `{{svg "dsi-name" 18}}` (stroke set in `web_src/svg/dsi-*.svg`, 24 grid, 1.75 stroke, `currentColor`). The prefix is `dsi-`, not `ds-`, so an icon's class never meets a component class. Octicons remain available for legacy pages.

## Settings shell: user, organization, repository, administration

Four layouts use one shell: `templates/{user,org,repo}/settings/layout_head.tmpl` and `templates/admin/layout_head.tmpl` (the `layout_footer` files close it). A settings page keeps `{{template "user/settings/layout_head" (dict "ctxData" . "pageClass" "…")}} … {{template "user/settings/layout_footer" .}}` and its content goes between them, inside `.ds-settings-body`; do not add a menu or a second tab strip.

Structure: `.ds-container.ds-settings` > `<nav class="ds-settings-nav" id="settings-menu">` + `.ds-settings-body`. From 1024 the menu is the sticky left column (250 px). Under 1024 the page shows a sub bar with a back arrow to `#settings-menu`, and the menu opens as a full screen list (`:target`, no script) with an identity card, grouped rows of 52 px with icon and chevron, and the rows open the pages (a list per page). Menu markup: `.ds-settings-who` (`snav_who`: `Entity` or `Icon`, `Name`, `Sub`, `Href` + `Label`), `<h2 class="ds-settings-group">`, `<ul class="ds-settings-list">` of `snav_item` (`Href`, `Label`, `Icon`, `Active`, `Danger`, `Badge`). Add a settings page by adding one `snav_item` to the area's `navbar.tmpl` and setting its `PageIs…` flag. Storage and AI keys are menu items, not tabs (`shared/asc/byo_head` has no tab strip).

In a settings page, keep the existing form markup (`.ui.form`, `.ui.segment`) until the page group rebuilds it; then use `.ds-card` with `.ds-card-h` title, `.ds-form` fields and a `.ds-sticky-actions` bar for Save and Discard.

## Pages built on it

Explore repositories, users and organizations (`templates/explore/*`, `shared/repo_search`), sign in, sign up and the account pages (`templates/user/auth/*`, opened with `user/auth/shell_start` and closed with `shell_end`: `Data Hook Eyebrow Title Lead Split NoAlert`, `Brand` = `signin|signup` for the navy panel from 1024), the landing page (`templates/home.tmpl`, `landing.css`), and the shells above. A new account-style page copies `signin.tmpl`; a new list page copies `explore/repos.tmpl` (sub bar, page head, tabs, filter bar, `.ds-split` with list and rail, `base/paginate`).

## Checks

* `go build ./... && go test ./routers/web/... ./modules/templates/...`
* `make frontend` builds the CSS; `make lint-frontend` lints it.
* Screenshots at 390, 768, 1024 and 1440, light and dark, with axe (WCAG 2.2 AA): no horizontal scroll, no serious finding in the shell.

## Build

The CSS is part of the normal frontend build (`make frontend`). Fonts: Inter and JetBrains Mono come first in the stack and fall back to the system fonts; no font file is requested, so the strict CSP stays unchanged.

## Older themes

`asc-auto`, `asc-light` and `asc-dark` (`web_src/css/themes/`) are the default account themes (`[ui] DEFAULT_THEME = asc-auto`); the DS tokens above are what pages read. The Forgejo themes stay available in the appearance settings; set `[ui] THEMES` explicitly only if you also list the asc names.
