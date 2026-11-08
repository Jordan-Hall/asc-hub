import {GET} from '../modules/fetch.js';

// The pattern field of the branch rule form (templates/repo/settings/protected_branch.tmpl). As you type it asks
// settings/branches/check which rule the pattern would overlap and how many open pull requests target a branch it
// matches, and shows both under the field. The save handler refuses an overlap on its own.

const waitMs = 300;

function show(form, check) {
  const input = form.querySelector('input[name="rule_name"]');
  const overlap = form.querySelector('[data-rule-overlap]');
  const one = form.querySelector('[data-rule-open-one]');
  const many = form.querySelector('[data-rule-open-many]');
  overlap.hidden = !check.overlap;
  overlap.querySelector('[data-rule-overlap-name]').textContent = check.overlap;
  input.setAttribute('aria-invalid', check.overlap ? 'true' : 'false');
  one.hidden = check.open !== 1;
  many.hidden = check.open < 2;
  many.querySelector('[data-rule-open-n]').textContent = String(check.open);
}

export function initAscRuleCheck({load = GET, delay = waitMs} = {}) {
  const form = document.querySelector('form[data-rule-check]');
  if (!form) return;
  const input = form.querySelector('input[name="rule_name"]');
  const ruleID = form.querySelector('input[name="rule_id"]').value;
  let timer = 0;
  let asked = 0;
  const ask = async () => {
    const turn = ++asked;
    const params = new URLSearchParams({rule_id: ruleID, rule_name: input.value.trim()});
    try {
      const res = await load(`${form.getAttribute('data-rule-check')}?${params}`);
      if (!res.ok || turn !== asked) return;
      show(form, await res.json());
    } catch {
      // the check is a help; the save handler refuses an overlap anyway
    }
  };
  input.addEventListener('input', () => {
    clearTimeout(timer);
    timer = setTimeout(ask, delay);
  });
}
