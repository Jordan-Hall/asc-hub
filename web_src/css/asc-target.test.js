// Target size (WCAG 2.2 2.5.8, QA O10): a control smaller than 24px keeps 24px around it with its margin, or sits in
// a label that is at least 24px.
import {readFileSync} from 'node:fs';
import {join} from 'node:path';
import postcss from 'postcss';

// vitest runs from the repository root
const parse = (path) => postcss.parse(readFileSync(join(process.cwd(), 'web_src/css', path), 'utf8'));

function decls(path, selector) {
  const out = {};
  parse(path).walkRules((rule) => {
    // top-level rules only: the coarse-pointer media queries make controls larger still
    if (rule.parent.type !== 'root' || rule.selector.replace(/\s+/g, ' ') !== selector) return;
    rule.walkDecls((d) => {
      out[d.prop] = d.value;
    });
  });
  return out;
}

const px = (value) => Number.parseFloat(value ?? '0');

// The margin box of a control: its size plus the margin on both sides (one value: every side).
function marginBox(d) {
  const [block, inline = block] = (d.margin ?? '0').split(/\s+/).map(px);
  return {width: px(d.width) + 2 * inline, height: px(d.height) + 2 * block};
}

const bare = [
  ['asc-pages/admin-base.css', '.adm-check-col input'],
  ['asc-pages/work-issues.css', '.wk-issue-check'],
  ['asc-pages/work-org.css', '.wk-units td input[type="radio"]'],
  ['asc-pages/review-ctl.css', '.ag-switch input[role="switch"]'],
];

test.each(bare)('%s %s keeps 24px around the control', (path, selector) => {
  const box = marginBox(decls(path, selector));
  expect(box.width).toBeGreaterThanOrEqual(24);
  expect(box.height).toBeGreaterThanOrEqual(24);
});

const labelled = [
  ['asc-ds/components.css', '.ds-check', 'min-height'],
  ['asc-pages/work-inbox.css', '.wk-irow-check', 'height'],
];

test.each(labelled)('%s %s label is at least 24px', (path, selector, prop) => {
  expect(px(decls(path, selector)[prop])).toBeGreaterThanOrEqual(24);
});

test('the switch knob stays inside the track when on', () => {
  const track = decls('asc-pages/review-ctl.css', '.ag-switch input[role="switch"]');
  const knob = decls('asc-pages/review-ctl.css', '.ag-switch input[role="switch"]::after');
  const on = decls('asc-pages/review-ctl.css', '.ag-switch input[role="switch"]:checked::after');
  const shift = px(/translateX\(([-\d.]+)px\)/.exec(on.transform)[1]);
  expect(px(knob['inset-inline-start']) * 2 + px(knob.width) + shift).toBe(px(track.width));
  const rtl = decls('asc-pages/review-ctl.css', '.ag-switch input[role="switch"]:checked:dir(rtl)::after');
  expect(rtl.transform).toBe(`translateX(-${shift}px)`);
});
