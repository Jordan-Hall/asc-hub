// The strength meter under a new password (sign up, reset, account linking). The markup is rendered by the hub as a
// list of rules (templates/user/auth/password_meter.tmpl): this ticks them and fills the bar while the person types.
// It only advises: the hub checks the same rules again when the form is sent.
//
// [data-password-meter]: data-for (id of the password field), data-min (shortest length), data-classes (required
// character classes, comma separated: lower, upper, digit, spec), data-t-empty / data-t-1..4 (level names),
// data-t-met / data-t-unmet (words for screen readers).

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

export function initAscPassword() {
  for (const meter of document.querySelectorAll('[data-password-meter]')) {
    const input = document.getElementById(meter.getAttribute('data-for'));
    if (!input) continue;
    const min = Number(meter.getAttribute('data-min')) || 8;
    const required = (meter.getAttribute('data-classes') ?? '').split(',').filter(Boolean);
    const text = (key) => meter.getAttribute(`data-t-${key}`) ?? '';
    const label = meter.querySelector('[data-pw-level]');
    const update = () => {
      const {level, rules} = checkPassword(input.value, {min, required});
      meter.setAttribute('data-level', String(level));
      label.textContent = level === 0 ? text('empty') : text(String(level));
      for (const item of meter.querySelectorAll('[data-rule]')) {
        const ok = Boolean(rules[item.getAttribute('data-rule')]);
        item.classList.toggle('is-met', ok);
        const state = item.querySelector('[data-pw-state]');
        if (state) state.textContent = text(ok ? 'met' : 'unmet');
      }
    };
    input.addEventListener('input', update);
    update();
  }
}
