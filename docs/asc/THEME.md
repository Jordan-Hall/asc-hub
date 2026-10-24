# ASC theme (ASC Hub)

ASC Hub ships three themes that follow the monorepo design system
(`packages/ui/src/theme.css`): `asc-auto`, `asc-light` and `asc-dark`.
They sit in `web_src/css/themes/` and are built on the Forgejo themes with the
`--color-*` variables overridden (cyan primary, slate neutrals, emerald accent,
asc semantic colours). They add a 4px accent bar on top of the navbar.
The Forgejo themes stay available.

## Make asc-auto the default

In `app.ini`:

```ini
[ui]
DEFAULT_THEME = asc-auto
```

Or with an environment variable (container deployments):

```sh
FORGEJO__ui__DEFAULT_THEME=asc-auto
```

`asc-auto` follows the system light/dark preference. Use `asc-light` or
`asc-dark` to force one. Users can still choose another theme in their
appearance settings; the three asc themes are in the default `THEMES` list.
If `[ui] THEMES` is set explicitly, add the asc names to it.

## Build

The CSS is part of the normal frontend build (`make frontend`).
