# ASC Hub branding

ASC Hub is a fork of Forgejo. The UI presents itself as ASC Hub; the license
attribution to Forgejo stays in the footer and in `/assets/licenses.txt`.

## What changed

| Area | Change |
| --- | --- |
| `assets/logo.svg`, `assets/favicon.svg` | ASC mark from the brand kit (tile `#125469`, white legs, cyan `#22d3ee` node). Source for `make generate-images`. |
| `public/assets/img/{logo,favicon}.svg` | Same marks, served as is. `logo-dark.svg` is the dark-surface variant (tile `#2394b2`, legs `#0b1220`); the header and dark pages use it. |
| `public/assets/img/{favicon,favicon-16,apple-touch-icon,logo,og-image,avatar_default}.png` | Copied from the brand kit: 32 px and 16 px favicons, 180 px apple touch icon, 512 px `logo.png` (PWA icon) and default avatar, 1200x630 `og-image.png` (the `og:image` fallback). |
| `public/assets/img/wordmark*.svg`, `logo-mark-mono.svg` | Kit wordmarks (ASC, ASC Hub; light and dark) and the one-colour mark. The landing page uses `wordmark-hub-dark.svg`. |
| `public/assets/img/forgejo-loading.svg` | Spinner drawn from the new mark. The file name is kept so that `install.tmpl`, `post-install.tmpl` and `repo/migrate/migrating.tmpl` need no edit. |
| `templates/base/head_navbar.tmpl`, `templates/status/500.tmpl` | Dark-surface mark plus the app name. |
| `templates/base/brand_mark.tmpl` | Theme-aware mark (light or dark tile) used above the sign-in and sign-up forms. |
| `templates/home.tmpl` | Landing page for asc: dark hero (tagline, `asc clone` example, register and sign-in buttons) and four cards for changesets, stacks, review and inbox. `home_forgejo.tmpl` is removed. |
| `templates/base/head.tmpl` | Title suffix, author, description and keywords use `APP_NAME` and the `startpage.meta_*` locale keys. `[ui.meta]` is no longer read by the page head. |
| `templates/base/head_opengraph.tmpl` | `og:title` fallback and `og:site_name` use `APP_NAME` (no slogan). |
| `templates/base/footer_content.tmpl` | "Powered by ASC Hub" and a small "Based on Forgejo" link (shown while `[other] SHOW_FOOTER_POWERED_BY` is true, the default), a "Why ASC Hub" link to [WHY.md](WHY.md) on the `asc/v16` branch, then the licenses, API and version links. |
| `options/locale/locale_en-US.ini`, `options/locale_next/locale_en-US.json` | `[startpage]` rewritten (new keys `title`, `meta_*`, `changesets*`, `stacks*`, `review*`, `inbox*`; old Forgejo marketing keys removed), new keys `based_on` and `why_asc_hub`, and "Forgejo" replaced by "ASC Hub" in user-facing sentences about the product (install, two-factor, authorized integrations, Actions). Names of other software stay (Forgejo Runner, the Forgejo webhook type, the Forgejo and Gitea themes and migration sources). Other languages are not edited; see [FORK.md](FORK.md#string-and-template-overrides). |
| `templates/swagger/ui.tmpl`, `templates/swagger/v1_json.tmpl`, `Makefile` | The API documentation is titled "ASC Hub API" (`SWAGGER_SPEC_BRANDING` and the committed spec change together). |

Kept as is: Go module path `forgejo.org`, `FORGEJO__` environment prefix,
cookie names, the `forgejo` CLI binary name and the `forgejo doctor` command,
the "Forgejo" webhook type (`settings.web_hook_name_forgejo`), links to
`forgejo.org/docs`, and every i18n key that existed before (other locales still
carry the removed `startpage` keys; they are unused and harmless).

Defaults in code: `modules/setting/server.go` defaults `APP_NAME` to "ASC Hub" and
`APP_SLOGAN` to empty, and `routers/install/install.go` pre-fills the install form with
"ASC Hub" and the slogan "Source control for people and their agents".
`tests/integration/footer_test.go` and `opengraph_test.go` expect the ASC Hub text.

The container's first-start template (`docker/rootless/usr/local/bin/docker-setup.sh`,
`docker/root/etc/s6/gitea/setup`) falls back to `ASC Hub` for `APP_NAME`, the
`custom/conf/app.example.ini` page-meta examples say ASC Hub, `cmd/main.go` has the usage line
"ASC Hub: source control for people and their agents", and the OCI labels in `Dockerfile` and
`Dockerfile.rootless` name ASC Hub (vendor ASC, source `https://github.com/Jordan-Hall/asc-hub`).
The test configurations (`tests/*.ini.tmpl`) use `APP_NAME = ASC Hub`.

Still reading Forgejo on purpose: the "Based on Forgejo" footer link, the Forgejo Runner and
webhook type names, the Forgejo and Gitea themes and migration sources, the admin
update-checker messages (they are about Forgejo releases, the upstream), the `forgejo`
command name, and the other languages' locale files.

## Regenerating images

The PNG files and the og image come straight from the brand kit and are not
regenerated. `make generate-images` (needs `node_modules`) would overwrite
`logo.png`, `favicon.png`, `avatar_default.png` and `apple-touch-icon.png` with
sharp renders of `assets/*.svg`; run it only if you want that, and copy the kit
files back otherwise. `og-image.png` and `favicon-16.png` are not touched.

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
