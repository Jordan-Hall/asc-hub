// Layout fit rules that only show on a narrow screen (QA O13): scrollers with a hidden scrollbar
// carry a scroll cue, and the profile repository list wraps its count instead of squeezing the name.
import {readFileSync} from 'node:fs';
import {join} from 'node:path';
import postcss from 'postcss';

// vitest runs from the repository root
const read = (path) => readFileSync(join(process.cwd(), path), 'utf8');
const parse = (path) => postcss.parse(read(path));

function decls(ast, selector, parentTest = () => true) {
  const out = {};
  ast.walkRules((rule) => {
    if (rule.selector.replace(/\s+/g, ' ') !== selector || !parentTest(rule.parent)) return;
    rule.walkDecls((d) => {
      out[d.prop] = d.value;
    });
  });
  return out;
}

const atRule = (name, params) => (parent) => parent?.type === 'atrule' && parent.name === name && parent.params === params;

describe('scroll cue', () => {
  const css = parse('web_src/css/asc-ds/components.css');

  test('fades the edge with more content', () => {
    const d = decls(css, '.ds-scroll-cue, .ds-cmd-scroll code');
    expect(d['mask-image']).toContain('var(--ds-fade-s)');
    expect(d['mask-image']).toContain('var(--ds-fade-e)');
  });

  test('a scroll timeline drives the fade', () => {
    const d = decls(css, '.ds-scroll-cue, .ds-cmd-scroll code', atRule('supports', '(animation-timeline: scroll())'));
    expect(d.animation).toBe('ds-scroll-fade linear both');
    expect(d['animation-timeline']).toBe('scroll(self inline)');
  });

  test('without scroll timelines the scrollbar shows', () => {
    const d = decls(css, '.ds-scroll-cue, .ds-cmd-scroll code', atRule('supports', 'not (animation-timeline: scroll())'));
    expect(d['scrollbar-width']).toBe('thin');
  });

  test('the fade lengths are registered so they animate', () => {
    const names = [];
    css.walkAtRules('property', (r) => {
      names.push(r.params);
    });
    expect(names).toEqual(expect.arrayContaining(['--ds-fade-s', '--ds-fade-e']));
  });
});

describe('repository tab strip', () => {
  const kit = parse('web_src/css/asc-pages/repo-kit.css');

  test('the strip carries the scroll cue', () => {
    expect(read('templates/repo/header_tabs.tmpl')).toContain('<div class="rp-tabs-in ds-scroll-cue">');
  });

  test('an open More sheet is not masked', () => {
    expect(decls(kit, '.rp-tabs-in:has(details[open])')['mask-image']).toBe('none');
  });

  test('the wrapped desktop strip drops the cue', () => {
    const d = decls(kit, '.rp-tabs .rp-tabs-in.ds-scroll-cue', atRule('media', '(min-width: 1024px)'));
    expect(d).toEqual({'mask-image': 'none', animation: 'none'});
  });
});

describe('profile side column', () => {
  const css = parse('web_src/css/asc-pages/work-profile.css');

  test('a repository row wraps its count under a long name', () => {
    expect(decls(css, '.wk-pf-repos li')['flex-wrap']).toBe('wrap');
    expect(decls(css, '.wk-pf-repos a').flex).toBe('1 1 12ch');
  });

  test('a long organization name breaks inside its row', () => {
    expect(decls(css, '.wk-pf-orgs a span')).toEqual({'min-width': '0', 'overflow-wrap': 'anywhere'});
  });
});
