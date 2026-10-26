# ASC theme (ASC Hub)

ASC Hub ships three themes: `asc-auto`, `asc-light` and `asc-dark`. They sit in
`web_src/css/themes/` and are the default (`[ui] DEFAULT_THEME = asc-auto`).
The Forgejo themes stay available. A user can pick another theme in the
appearance settings; set `[ui] THEMES` explicitly only if you also list the asc
names.

## Palette

| Token | Light | Dark | Use |
| --- | --- | --- | --- |
| primary | `#125469` | `#2394b2` | links, primary buttons, brand tile |
| header | `#0b1220` | `#0b1220` | header and landing hero, in both themes |
| accent | `#22d3ee` | `#22d3ee` | focus ring on dark surfaces, active tab, notification badge, hero button fill |
| page | `#f6f8fa` | `#0e1626` | page background |
| card | `#ffffff` | `#151f33` | cards, boxes, menus |
| text | `#0f172a` | `#e6edf6` | body text |
| muted text | `#475569` | `#9fb0c6` | secondary text |
| border | `#d8dee6` | `#26324a` | card borders |

Contrast: body and muted text are at least 4.5:1 on every surface they sit on
(primary on light is 7.5 to 8.4:1; `#2394b2` on the dark page and cards is 4.7
to 5.1:1). Form control borders are at least 3:1. The cyan accent is never used
as text on a light surface. The neutral scales (`--zinc-*` in the light theme,
`--steel-*` in the dark theme) hold slate and navy values.

Shape: cards, segments, dialogs and headers use `--border-radius-card: 12px`;
buttons use 6px. The font stack puts Inter and JetBrains Mono first and falls
back to the system fonts. No font file is requested, so the strict CSP is
unchanged: the fonts show up where they are installed.

## Mobile first

`web_src/css/asc.css` and the landing page, footer, button, flex-container and
repository header rules are written mobile first: the base rules are for
320 to 390 px and `min-width` queries add columns. Buttons and menu items are 44 px
high (36 px on a wide screen with a precise pointer), the header keeps its
menu button on phones, code and the clone command scroll inside their own
block, tables scroll inside their own box (add class `asc-stack` and `data-label`
to the cells of a table that should become stacked cards), safe-area insets pad
the header, hero, cards and footer, and transitions stop under
`prefers-reduced-motion`.

## Build

The CSS is part of the normal frontend build (`make frontend`).
