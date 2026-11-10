// WCAG 2.2 AA text contrast (4.5:1) of the colour tokens, in light and dark (QA O10). The pairs are
// the text colours each theme puts on its own surfaces; a new token pair that fails shows here
// before a browser audit finds it on a page.
import {readFileSync} from 'node:fs';
import {join} from 'node:path';

const read = (path) => readFileSync(join(process.cwd(), path), 'utf8');

// the custom properties of each rule block, in order; a later block overrides an earlier one
function blocks(css) {
  const out = [];
  for (const [, selector, body] of css.matchAll(/([^{}]*)\{([^{}]*)\}/g)) {
    const props = {};
    for (const [, name, value] of body.matchAll(/--([a-z0-9-]+):\s*([^;]+);/g)) props[name] = value.trim();
    out.push({selector: selector.trim(), props});
  }
  return out;
}

function resolve(props, value, depth = 0) {
  const ref = /^var\(--([a-z0-9-]+)(?:,\s*(.+))?\)$/.exec(value);
  if (!ref || depth > 8) return value;
  return resolve(props, props[ref[1]] ?? ref[2], depth + 1);
}

const srgbToLinear = (c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);

// linear sRGB of #rgb, #rrggbb or oklch(L C Hdeg); alpha is ignored (the pairs below are opaque)
function linear(props, value) {
  const v = resolve(props, value);
  const hex = /^#([0-9a-f]{3,8})$/i.exec(v);
  if (hex) {
    let h = hex[1];
    if (h.length <= 4) h = h.split('').map((c) => c + c).join('');
    return [0, 2, 4].map((i) => srgbToLinear(Number.parseInt(h.slice(i, i + 2), 16) / 255));
  }
  const ok = /^oklch\((\S+)\s+(\S+)\s+([\d.]+)deg\)$/.exec(v);
  if (ok) {
    const [l, c] = [ok[1], ok[2]].map((x) => Number(resolve(props, x)));
    const h = Number(ok[3]) * Math.PI / 180;
    const [a, b] = [c * Math.cos(h), c * Math.sin(h)];
    const l1 = (l + 0.3963377774 * a + 0.2158037573 * b) ** 3;
    const m1 = (l - 0.1055613458 * a - 0.0638541728 * b) ** 3;
    const s1 = (l - 0.0894841775 * a - 1.291485548 * b) ** 3;
    return [
      4.0767416621 * l1 - 3.3077115913 * m1 + 0.2309699292 * s1,
      -1.2684380046 * l1 + 2.6097574011 * m1 - 0.3413193965 * s1,
      -0.0041960863 * l1 - 0.7034186147 * m1 + 1.707614701 * s1,
    ].map((x) => Math.min(1, Math.max(0, x)));
  }
  throw new Error(`not a colour: ${value} (${v})`);
}

const lum = (rgb) => 0.2126 * rgb[0] + 0.7152 * rgb[1] + 0.0722 * rgb[2];

function contrast(props, fg, bg) {
  const [x, y] = [lum(linear(props, `var(--${fg})`)), lum(linear(props, `var(--${bg})`))];
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

function failing(props, pairs) {
  return pairs
    .map(([fg, bg]) => [fg, bg, contrast(props, fg, bg)])
    .filter(([, , c]) => c < 4.5)
    .map(([fg, bg, c]) => `${fg} on ${bg}: ${c.toFixed(2)}`);
}

describe('design tokens', () => {
  const all = blocks(read('web_src/css/asc-ds/tokens.css'));
  const light = all.find((b) => b.props.text && b.props.bg).props;
  const dark = {...light, ...all.find((b) => b.selector.endsWith(':root[data-theme*="dark"]')).props};
  const text = ['text', 'text-1', 'text-b', 'text-2', 'text-3', 'link', 'ok', 'warn', 'err', 'agent'];
  const surfaces = ['bg', 'surface', 'surface-2', 'surface-3', 'canvas', 'hover'];
  const pairs = [
    ...text.flatMap((fg) => surfaces.map((bg) => [fg, bg])),
    ['ok', 'ok-soft'], ['warn', 'warn-soft'], ['err', 'err-soft'], ['agent', 'agent-soft'], ['agent', 'agent-tint'],
    ['link', 'primary-soft'], ['link', 'primary-tint'], ['on-fill', 'fill'], ['on-fill', 'fill-hover'],
    ['add-ink', 'add-bg'], ['add-ink', 'add-gut'], ['del-ink', 'del-bg'], ['del-ink', 'del-gut'], ['note-ink', 'note-bg'],
    ['inv-text', 'inv-bg'], ['navy-text', 'navy'], ['navy-text-2', 'navy'], ['navy-text-2', 'navy-field'],
    ['term-text', 'navy'], ['term-gutter', 'navy'], ['term-comment', 'navy'], ['term-prompt', 'navy'],
  ];

  test('light', () => expect(failing(light, pairs)).toEqual([]));
  test('dark', () => expect(failing(dark, pairs)).toEqual([]));
});

describe('page themes', () => {
  const text = ['color-text', 'color-text-light', 'color-text-light-2', 'color-text-light-3', 'color-primary',
    'color-red', 'color-green', 'color-orange', 'color-blue'];
  const surfaces = ['color-body', 'color-box-body', 'color-box-header', 'color-menu', 'color-box-body-highlight'];
  const pairs = [
    ...text.flatMap((fg) => surfaces.map((bg) => [fg, bg])),
    ['color-primary-contrast', 'color-primary'],
    // `.text.purple` and the basic purple label and button take the thin purple when a theme has one
    ...surfaces.map((bg) => ['purple-text', bg]),
  ];
  test.each(['light', 'dark'])('%s', (theme) => {
    const props = Object.assign({}, ...blocks(read(`web_src/css/themes/theme-asc-${theme}.css`)).map((b) => b.props));
    props['purple-text'] = 'var(--color-thin-purple, var(--color-purple))';
    expect(failing(props, pairs)).toEqual([]);
  });
});

test('the basic purple label and button use the thin purple', () => {
  expect(read('web_src/css/modules/label.css')).toMatch(/\.basic\.purple\.label \{[^}]*color: var\(--color-thin-purple, var\(--color-purple\)\);/);
  expect(read('web_src/css/modules/button-legacy.css')).toMatch(/\.basic\.purple\.button:focus \{[^}]*color: var\(--color-thin-purple, var\(--color-purple\)\);/);
});
