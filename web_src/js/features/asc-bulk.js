/**
 * A list with ticks and a bar of bulk actions (form[data-bulk]): the bar says how many are ticked, its buttons wait for
 * one tick, and the head tick ticks every row. Without script the bar still sends what is ticked.
 */
export function syncBulk(form, doc = document) {
  const rows = [...doc.querySelectorAll(`input[data-bulk-row][form="${form.id}"]`)];
  const n = rows.filter((r) => r.checked).length;
  const count = form.querySelector('[data-bulk-count]');
  if (count && n > 0) count.textContent = (n === 1 ? count.dataset.one : count.dataset.many).replace('%d', String(n));
  else if (count) count.textContent = count.dataset.hint ?? count.textContent;
  for (const b of form.querySelectorAll('[data-bulk-action]')) b.disabled = n === 0;
  const all = doc.querySelector('input[data-bulk-all]');
  if (all) {
    all.checked = n > 0 && n === rows.length;
    all.indeterminate = n > 0 && n < rows.length;
  }
  return n;
}

export function initAscBulk(doc = document) {
  const form = doc.querySelector('form[data-bulk]');
  if (!form) return;
  const count = form.querySelector('[data-bulk-count]');
  if (count) count.dataset.hint = count.textContent;
  doc.addEventListener('change', (e) => {
    const t = e.target;
    if (t.matches?.('input[data-bulk-all]')) {
      for (const r of doc.querySelectorAll(`input[data-bulk-row][form="${form.id}"]`)) r.checked = t.checked;
    }
    if (t.matches?.('input[data-bulk-all], input[data-bulk-row]')) syncBulk(form, doc);
  });
  syncBulk(form, doc);
}
