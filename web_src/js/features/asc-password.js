// The strength meter under a new password (sign up, reset, account linking). The markup is rendered by the hub as a
// list of rules (templates/user/auth/password_meter.tmpl): this ticks them and fills the bar while the person types.
// It only advises: the hub checks the same rules again when the form is sent.
//
// [data-password-meter]: data-for (id of the password field), data-min (shortest length), data-classes (required
// character classes, comma separated: lower, upper, digit, spec), data-t-empty / data-t-1..4 (level names),
// data-t-met / data-t-unmet (words for screen readers), data-range-url (when the hub checks breached passwords: the
// breach rule asks it for the hashes that share the first five digits of the SHA-1 of the password, so the password and
// its full hash never leave the browser).

import {GET} from '../modules/fetch.js';

const CLASS_TESTS = {
  lower: (pw) => /[a-z]/.test(pw),
  upper: (pw) => /[A-Z]/.test(pw),
  digit: (pw) => /\d/.test(pw),
  spec: (pw) => /[^a-zA-Z0-9]/.test(pw),
};

export function classesOf(pw) {
  return Object.keys(CLASS_TESTS).filter((name) => CLASS_TESTS[name](pw));
}

// level: 0 nothing typed, 1 weak (a rule is not met, or one character repeated), 2 fair (rules met), 3 good (12 or more
// characters and three kinds), 4 strong (16 or more and three kinds, or 24 or more).
export function checkPassword(pw, {min = 8, required = []} = {}) {
  const present = classesOf(pw);
  const rules = {length: pw.length >= min};
  for (const name of required) rules[name] = CLASS_TESTS[name]?.(pw) ?? true;
  rules.mix = present.length >= 3;
  const met = Object.entries(rules).every(([name, ok]) => ok || name === 'mix');
  let level = 0;
  if (pw.length > 0) {
    level = 1;
    const repeated = new Set(pw).size <= 2 && pw.length > 2;
    if (met && !repeated) {
      level = 2;
      if (pw.length >= 12 && present.length >= 3) level = 3;
      if ((pw.length >= 16 && present.length >= 3) || pw.length >= 24) level = 4;
    }
  }
  return {level, rules};
}

// sha1Hex is the SHA-1 of pw in upper case hexadecimal.
export async function sha1Hex(pw) {
  const digest = await crypto.subtle.digest('SHA-1', new TextEncoder().encode(pw));
  return Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, '0')).join('').toUpperCase();
}

// breachChecker asks url for the range of a password: true when it is not in a known breach, false when it is or the
// hub could not tell. The ranges are kept, so typing on asks once per prefix.
export function breachChecker(url) {
  const ranges = new Map();
  return async (pw) => {
    const hash = await sha1Hex(pw);
    const prefix = hash.slice(0, 5);
    if (!ranges.has(prefix)) {
      const resp = await GET(`${url}/${prefix}`);
      if (!resp.ok) return false;
      ranges.set(prefix, new Set((await resp.text()).split('\n').filter(Boolean)));
    }
    return !ranges.get(prefix).has(hash.slice(5));
  };
}

const BREACH_DELAY = 400;

// initMeter ticks the rules of meter while the person types in input.
function initMeter(meter, input) {
  const min = Number(meter.getAttribute('data-min')) || 8;
  const required = (meter.getAttribute('data-classes') ?? '').split(',').filter(Boolean);
  const text = (key) => meter.getAttribute(`data-t-${key}`) ?? '';
  const label = meter.querySelector('[data-pw-level]');
  const rangeUrl = meter.getAttribute('data-range-url');
  const checkBreach = rangeUrl ? breachChecker(rangeUrl) : null;
  let clean = null; // the password the breach rule was last checked for, and whether it is clean
  let seq = 0; // the newest check: an older answer is dropped
  let timer = 0;
  const render = () => {
    const pw = input.value;
    const {level, rules} = checkPassword(pw, {min, required});
    let shown = level;
    if (checkBreach) {
      rules.breach = clean?.pw === pw && clean.ok;
      // a breached password is weak whatever else it has
      if (clean?.pw === pw && !clean.ok && level > 1) shown = 1;
    }
    meter.setAttribute('data-level', String(shown));
    label.textContent = shown === 0 ? text('empty') : text(String(shown));
    for (const item of meter.querySelectorAll('[data-rule]')) {
      const ok = Boolean(rules[item.getAttribute('data-rule')]);
      item.classList.toggle('is-met', ok);
      const state = item.querySelector('[data-pw-state]');
      if (state) state.textContent = text(ok ? 'met' : 'unmet');
    }
  };
  const breach = async () => {
    const pw = input.value;
    const mine = ++seq;
    let ok = false;
    try {
      ok = await checkBreach(pw);
    } catch {
      ok = false;
    }
    if (mine !== seq) return;
    clean = {pw, ok};
    render();
  };
  const update = () => {
    render();
    if (!checkBreach) return;
    clearTimeout(timer);
    seq++;
    if (input.value.length >= min) timer = setTimeout(breach, BREACH_DELAY);
  };
  input.addEventListener('input', update);
  update();
}

export function initAscPassword() {
  for (const meter of document.querySelectorAll('[data-password-meter]')) {
    const input = document.getElementById(meter.getAttribute('data-for'));
    if (input) initMeter(meter, input);
  }
}
