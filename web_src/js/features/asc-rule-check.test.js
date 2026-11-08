import {initAscRuleCheck} from './asc-rule-check.js';

function page() {
  document.body.innerHTML = `<form data-rule-check="/o/r/settings/branches/check"><input name="rule_id" value="7">
<input name="rule_name" value="main">
<p data-rule-overlap hidden>Rules cannot overlap: <b data-rule-overlap-name></b></p>
<p data-rule-open-one hidden>1 open</p><p data-rule-open-many hidden><b data-rule-open-n></b> open</p></form>`;
  return document.querySelector('input[name="rule_name"]');
}

function answer(body) {
  return vi.fn(() => Promise.resolve({ok: true, json: () => Promise.resolve(body)}));
}

async function typeAndWait(input, value) {
  input.value = value;
  input.dispatchEvent(new Event('input'));
  await new Promise((resolve) => setTimeout(resolve, 5));
  await Promise.resolve();
}

test('an overlapping pattern shows the rule it overlaps and marks the field', async () => {
  const input = page();
  const load = answer({overlap: 'release/*', open: 3});
  initAscRuleCheck({load, delay: 0});
  await typeAndWait(input, ' release/1 ');
  expect(load).toHaveBeenCalledWith('/o/r/settings/branches/check?rule_id=7&rule_name=release%2F1');
  expect(document.querySelector('[data-rule-overlap]').hidden).toBe(false);
  expect(document.querySelector('[data-rule-overlap-name]').textContent).toBe('release/*');
  expect(input.getAttribute('aria-invalid')).toBe('true');
  expect(document.querySelector('[data-rule-open-one]').hidden).toBe(true);
  expect(document.querySelector('[data-rule-open-many]').hidden).toBe(false);
  expect(document.querySelector('[data-rule-open-n]').textContent).toBe('3');
});

test('a free pattern with one open pull request shows the singular line', async () => {
  const input = page();
  initAscRuleCheck({load: answer({overlap: '', open: 1}), delay: 0});
  await typeAndWait(input, 'dev');
  expect(document.querySelector('[data-rule-overlap]').hidden).toBe(true);
  expect(input.getAttribute('aria-invalid')).toBe('false');
  expect(document.querySelector('[data-rule-open-one]').hidden).toBe(false);
  expect(document.querySelector('[data-rule-open-many]').hidden).toBe(true);
});

test('a failed check changes nothing', async () => {
  const input = page();
  initAscRuleCheck({load: vi.fn(() => Promise.reject(new Error('offline'))), delay: 0});
  await typeAndWait(input, 'dev');
  expect(document.querySelector('[data-rule-overlap]').hidden).toBe(true);
  expect(input.hasAttribute('aria-invalid')).toBe(false);
});
