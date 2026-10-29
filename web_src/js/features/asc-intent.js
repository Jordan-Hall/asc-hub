// Intent template of a new change (board NewChange): a button with data-asc-intent and the four headings in data-what,
// data-why, data-how and data-risk puts "## What", "## Why", "## How to check" and "## Risk" into the description
// (the textarea named by data-asc-intent-target) with a blank line under each, ready to fill in. A description that is
// not empty keeps its text and the template goes below it. The write and preview tabs are those of the markdown editor.

export function intentTemplate(h) {
  return [h.what, h.why, h.how, h.risk].map((x) => `## ${x}\n\n`).join('\n');
}

export function applyIntent(btn) {
  const target = document.querySelector(btn.getAttribute('data-asc-intent-target') || '');
  if (!target) return false;
  const tpl = intentTemplate({what: btn.dataset.what, why: btn.dataset.why, how: btn.dataset.how, risk: btn.dataset.risk});
  const current = target.value.trim();
  target.value = current ? `${target.value.replace(/\s+$/, '')}\n\n${tpl}` : tpl;
  target.dispatchEvent(new Event('input', {bubbles: true}));
  target.focus();
  // the cursor goes under the first heading
  const at = target.value.indexOf(`## ${btn.dataset.what}\n\n`) + `## ${btn.dataset.what}\n\n`.length;
  target.setSelectionRange(at, at);
  return true;
}

export function initAscIntent() {
  document.addEventListener('click', (e) => {
    const btn = e.target instanceof Element ? e.target.closest('[data-asc-intent]') : null;
    if (btn) {
      e.preventDefault();
      applyIntent(btn);
    }
  });
}
