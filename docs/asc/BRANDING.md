# ASC Hub branding

ASC Hub is a fork of Forgejo. The UI presents itself as ASC Hub; the license
attribution to Forgejo stays in the footer and in `/assets/licenses.txt`.

## What changed

| Area | Change |
| --- | --- |
| `assets/logo.svg`, `assets/favicon.svg` | ASC mark: five figures, `#125469` on light, `#22d3ee` on dark (`prefers-color-scheme` inside the SVG). Source for `make generate-images`. |
| `public/assets/img/{logo,favicon}.svg` | Same mark, ready to serve. |
| `public/assets/img/{favicon,apple-touch-icon,logo,avatar_default}.png` | Rendered from the same geometry (favicon transparent, the others on slate `#0f172a`). `logo.png` is the `og:image` and PWA icon. |
| `public/assets/img/forgejo-loading.svg` | ASC spinner. The file name is kept so that `install.tmpl`, `post-install.tmpl` and `repo/migrate/migrating.tmpl` need no edit. |
| `templates/home.tmpl` | Landing page for asc: changesets, stacks, review, inbox, plus sign-in and register buttons. `home_forgejo.tmpl` is removed. |
| `templates/base/head.tmpl` | Title suffix, author, description and keywords use `APP_NAME` and the `startpage.meta_*` locale keys. `[ui.meta]` is no longer read by the page head. |
| `templates/base/head_opengraph.tmpl` | `og:title` fallback and `og:site_name` use `APP_NAME` (no slogan). |
| `templates/base/footer_content.tmpl` | "Powered by ASC Hub" and a small "Based on Forgejo" link (shown while `[other] SHOW_FOOTER_POWERED_BY` is true, the default). Licenses, API and version links are unchanged. |
| `options/locale/locale_en-US.ini` | `[startpage]` rewritten (new keys `title`, `meta_*`, `changesets*`, `stacks*`, `review*`, `inbox*`; old Forgejo marketing keys removed), new key `based_on`, and "Forgejo" replaced by "ASC Hub" in user-facing strings. |

Kept as is: Go module path `forgejo.org`, `FORGEJO__` environment prefix,
cookie names, the `forgejo` CLI binary name and the `forgejo doctor` command,
the "Forgejo" webhook type (`settings.web_hook_name_forgejo`), links to
`forgejo.org/docs`, and every i18n key that existed before (other locales still
carry the removed `startpage` keys; they are unused and harmless).

Not changed here (Go code or tests, outside the branding patch):
`modules/setting/ui.go` still has Forgejo defaults for `[ui.meta]`,
`modules/setting/server.go` defaults `APP_NAME` to "Forgejo: Beyond coding. We
Forge.", `routers/install/install.go` pre-fills the slogan, `cmd/main.go` has
the CLI usage line. Set `APP_NAME = ASC Hub` in `app.ini` (the page head then
needs nothing else). `tests/integration/footer_test.go` expects "Powered by
Forgejo" and needs the new text.

## Regenerating images

`make generate-images` (needs `node_modules`) rebuilds the SVG and PNG files
from `assets/*.svg` with sharp. Sharp renders the light colour only, so the PNGs
committed here were drawn with the same geometry on a slate background for
contrast. Re-run the generator only if you also accept that.

## Overriding with the `custom/` directory

Forgejo reads overrides from `custom/` (`FORGEJO_CUSTOM`, default
`<work path>/custom`). No rebuild is needed.

- Images: put files under `custom/public/assets/img/`, for example `logo.svg`,
  `logo.png`, `favicon.svg`, `favicon.png`, `apple-touch-icon.png`.
- Landing page: copy `templates/home.tmpl` to `custom/templates/home.tmpl`.
- Footer links or scripts: `custom/templates/custom/extra_links_footer.tmpl`,
  `custom/templates/custom/header.tmpl`.
- Strings: `custom/options/locale/locale_en-US.ini` overrides single keys, for
  example `startpage.app_desc` or `startpage.title`.
- Site name: `APP_NAME` in `app.ini`. Do not set `APP_SLOGAN` if you want the
  bare name in titles (the head uses `APP_NAME` only).

Keep the "Based on Forgejo" link and the licenses link when you override the
footer; they are the license attribution.
