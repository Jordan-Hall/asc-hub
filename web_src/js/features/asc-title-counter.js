// Title counter of a new change (board NewChange): an input with data-asc-count="<id of the output element>"
// and data-asc-count-max="120" shows "68 of 120" while the person types, and marks the field invalid (aria-invalid and
// the message of data-asc-count-over in the output) when the title is longer than the advice. The counts are of
// characters, not bytes. Nothing is blocked: the server decides what it accepts.

export function countChars(value) {
  return Array.from(String(value ?? '')).length;
}

export function describeCount(n, max, template) {
  return String(template || '%d of %d').replace('%d', String(n)).replace('%d', String(max));
}

export function updateCounter(input, out) {
  const max = Number.parseInt(input.getAttribute('data-asc-count-max') || '', 10);
  if (!Number.isFinite(max) || max <= 0) return null;
  const n = countChars(input.value);
  const over = n > max;
  input.setAttribute('aria-invalid', String(over));
  const base = describeCount(n, max, input.getAttribute('data-asc-count-template'));
  out.textContent = over ? `${base}. ${input.getAttribute('data-asc-count-over') || ''}`.trim() : base;
  out.classList.toggle('ds-err-msg', over);
  return {n, max, over};
}

export function initAscTitleCounter() {
  for (const input of document.querySelectorAll('input[data-asc-count]')) {
    const out = document.getElementById(input.getAttribute('data-asc-count'));
    if (!out) continue;
    const run = () => updateCounter(input, out);
    input.addEventListener('input', run);
    run();
  }
}
