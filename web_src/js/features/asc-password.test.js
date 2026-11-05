import {breachChecker, checkPassword, classesOf, initAscPassword, sha1Hex} from './asc-password.js';

test('classesOf', () => {
  expect(classesOf('')).toEqual([]);
  expect(classesOf('aB3!')).toEqual(['lower', 'upper', 'digit', 'spec']);
  expect(classesOf('abc def')).toEqual(['lower', 'spec']);
});

test('checkPassword levels', () => {
  expect(checkPassword('').level).toBe(0);
  expect(checkPassword('short', {min: 8}).level).toBe(1);
  expect(checkPassword('aaaaaaaaaaaa', {min: 8}).level).toBe(1);
  expect(checkPassword('longenough', {min: 8}).level).toBe(2);
  expect(checkPassword('Longer-pass1', {min: 8}).level).toBe(3);
  expect(checkPassword('Correct-horse-battery9', {min: 8}).level).toBe(4);
  expect(checkPassword(`${'a'.repeat(12)}bcdefghijklmnopqrstuvwx`, {min: 8}).level).toBe(4);
});

test('required classes keep the level at weak until they are present', () => {
  const required = ['lower', 'upper', 'digit'];
  const lower = checkPassword('onlylowercase', {min: 8, required});
  expect(lower.level).toBe(1);
  expect(lower.rules).toMatchObject({length: true, lower: true, upper: false, digit: false});
  expect(checkPassword('Onlylower1', {min: 8, required}).level).toBe(2);
});

test('sha1Hex', async () => {
  expect(await sha1Hex('password')).toBe('5BAA61E4C9B93F3F0682250B6CF8331B7EE68FD8');
});

test('breachChecker asks once per prefix and sends only the prefix', async () => {
  const calls = [];
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (url) => {
    calls.push(String(url));
    return new Response('1E4C9B93F3F0682250B6CF8331B7EE68FD8\n0000000000000000000000000000000000A\n', {status: 200});
  });
  try {
    const check = breachChecker('/-/password/range');
    expect(await check('password')).toBe(false);
    expect(await check('password')).toBe(false);
    expect(calls).toEqual(['/-/password/range/5BAA6']);
    expect(await check('Correct-horse-battery9')).toBe(true);
    expect(calls).toHaveLength(2);
    expect(calls.join(' ')).not.toContain('password/range/5BAA61E4');
  } finally {
    fetchMock.mockRestore();
  }
});

test('breachChecker treats a failed answer as not clean', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async () => new Response('', {status: 502}));
  try {
    expect(await breachChecker('/r')('anything-long')).toBe(false);
  } finally {
    fetchMock.mockRestore();
  }
});

test('a breached password is weak and leaves the breach rule unmet', async () => {
  document.body.innerHTML = `<input id="pw">
    <div data-password-meter data-for="pw" data-min="8" data-classes="" data-range-url="/r" data-t-1="Weak" data-t-3="Good">
      <span data-pw-level></span><ul><li data-rule="length"></li><li data-rule="breach"></li></ul></div>`;
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async () => new Response('', {status: 200}));
  vi.useFakeTimers();
  try {
    initAscPassword();
    const q = (sel) => document.querySelector(sel);
    const input = q('#pw');
    input.value = 'Longer-pass1';
    input.dispatchEvent(new Event('input'));
    expect(q('[data-rule="breach"]').classList.contains('is-met')).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(500);
    await vi.waitFor(() => expect(q('[data-rule="breach"]').classList.contains('is-met')).toBe(true));
    expect(q('[data-password-meter]').getAttribute('data-level')).toBe('3');

    const hash = await sha1Hex('Longer-pass2');
    fetchMock.mockImplementation(async () => new Response(`${hash.slice(5)}\n`, {status: 200}));
    input.value = 'Longer-pass2';
    input.dispatchEvent(new Event('input'));
    await vi.advanceTimersByTimeAsync(500);
    await vi.waitFor(() => expect(q('[data-password-meter]').getAttribute('data-level')).toBe('1'));
    expect(q('[data-pw-level]').textContent).toBe('Weak');
    expect(q('[data-rule="breach"]').classList.contains('is-met')).toBe(false);
  } finally {
    vi.useRealTimers();
    fetchMock.mockRestore();
  }
});
