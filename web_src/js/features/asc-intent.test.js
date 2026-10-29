import {intentTemplate, applyIntent, initAscIntent} from './asc-intent.js';

const h = {what: 'What', why: 'Why', how: 'How to check', risk: 'Risk'};

test('the template has the four headings, each with room under it', () => {
  expect(intentTemplate(h)).toBe('## What\n\n\n## Why\n\n\n## How to check\n\n\n## Risk\n\n');
});

function page(text) {
  document.body.innerHTML = `<textarea name="content">${text}</textarea><button type="button" data-asc-intent data-asc-intent-target="textarea[name=content]" data-what="What" data-why="Why" data-how="How to check" data-risk="Risk">T</button>`;
  return [document.querySelector('textarea'), document.querySelector('button')];
}

test('an empty description gets the template with the cursor under What', () => {
  const [ta, btn] = page('');
  expect(applyIntent(btn)).toBe(true);
  expect(ta.value.startsWith('## What\n\n')).toBe(true);
  expect(ta.value).toContain('## Risk');
  expect(ta.selectionStart).toBe('## What\n\n'.length);
});

test('a description with text keeps it and gets the template below', () => {
  const [ta, btn] = page('Already written.');
  applyIntent(btn);
  expect(ta.value.startsWith('Already written.\n\n## What')).toBe(true);
});

test('a button without a target does nothing, a click applies the template', () => {
  document.body.innerHTML = '<button type="button" data-asc-intent data-asc-intent-target="#none">T</button>';
  expect(applyIntent(document.querySelector('button'))).toBe(false);
  const [ta, btn] = page('');
  initAscIntent();
  btn.click();
  expect(ta.value).toContain('## Why');
});
