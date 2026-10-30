// The secret scanning page (templates/repo/settings/asc_security.tmpl): "Scan main now" shows its progress where it is, and a
// finding set to a state leaves the list with a toast that can undo it. Without this script the same forms submit and reload.
import {POST} from '../modules/fetch.js';

/** A version 7 uuid, the operation id the server wants for a change that can be retried. */
export function uuidv7(now = Date.now(), rand = (n) => crypto.getRandomValues(new Uint8Array(n))) {
  const b = rand(16);
  let t = BigInt(now);
  for (let i = 5; i >= 0; i--) {
    b[i] = Number(t & 0xffn);
    t >>= 8n;
  }
  b[6] = (b[6] & 0x0f) | 0x70;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

/** Fills the %s of a message with the given words. Text only: the caller puts the result into textContent. */
export function fill(message, ...words) {
  let i = 0;
  return (message || '').replace(/%s/g, () => words[i++] ?? '');
}

async function post(url, body) {
  const res = await POST(url, {data: body, headers: {Accept: 'application/json'}});
  let data = {};
  try {
    data = await res.json();
  } catch {
    // not JSON: a failure page
  }
  return {ok: res.ok && data.ok !== false, message: data.message || ''};
}

/** The words for a state key (open, revoked, false_positive, accepted_risk) from the data-i-state-* attributes of the page. */
export function stateName(data, key) {
  return data[`iState${key.charAt(0).toUpperCase()}${key.slice(1)}`] || key;
}

function bump(root, key, by) {
  const n = root.querySelector(`[data-testid="asc-findings-tab-${key}"] .rv-count`);
  if (n) n.textContent = String(Math.max(0, (Number.parseInt(n.textContent) || 0) + by));
}

export function showUndoToast({title, detail, undoLabel, onUndo}) {
  document.querySelector('.rv-undo-toast')?.remove();
  const toast = document.createElement('div');
  toast.className = 'rv-undo-toast';
  toast.setAttribute('role', 'status');
  const text = document.createElement('div');
  const b = document.createElement('b');
  b.textContent = title;
  const s = document.createElement('span');
  s.textContent = detail;
  text.append(b, s);
  const undo = document.createElement('button');
  undo.type = 'button';
  undo.textContent = undoLabel;
  toast.append(text, undo);
  document.body.append(toast);
  const timer = setTimeout(() => toast.remove(), 12000);
  const hold = () => clearTimeout(timer);
  toast.addEventListener('mouseenter', hold);
  toast.addEventListener('focusin', hold);
  undo.addEventListener('click', async () => {
    undo.disabled = true;
    await onUndo(toast);
  });
  return toast;
}

function initScan(root) {
  const form = root.querySelector('form[data-asc-scan]');
  const box = root.querySelector('[data-asc-scanning]');
  if (!form || !box) return;
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const btn = form.querySelector('button[type=submit]');
    const label = box.querySelector('[data-asc-scan-label]');
    const clock = box.querySelector('[data-asc-scan-clock]');
    btn.disabled = true;
    btn.setAttribute('aria-busy', 'true');
    box.hidden = false;
    label.textContent = fill(box.dataset.running, form.dataset.branch || '');
    const started = Date.now();
    const tick = setInterval(() => {
      clock.textContent = `${Math.floor((Date.now() - started) / 1000)} s`;
    }, 1000);
    let res;
    try {
      res = await post(form.action, new FormData(form));
    } catch {
      res = {ok: false, message: ''};
    }
    clearInterval(tick);
    label.textContent = res.ok ? (res.message || box.dataset.done) : (res.message || box.dataset.failed);
    box.querySelector('.rv-scanning-bar')?.setAttribute('hidden', '');
    if (res.ok) {
      setTimeout(() => window.location.reload(), 1200);
    } else {
      btn.disabled = false;
      btn.removeAttribute('aria-busy');
    }
  });
}

function initFindings(root) {
  const t = root.dataset;
  for (const form of root.querySelectorAll('.rv-finding-form')) {
    form.addEventListener('submit', async (e) => {
      const btn = e.submitter;
      if (!btn || btn.name !== 'state') return;
      e.preventDefault();
      const row = form.closest('.rv-finding');
      const prev = form.dataset.state;
      const next = btn.value;
      const where = row?.dataset.ascKey || '';
      const data = new FormData(form);
      data.set('state', next);
      for (const x of form.querySelectorAll('button')) x.disabled = true;
      let res;
      try {
        res = await post(form.action, data);
      } catch {
        res = {ok: false, message: ''};
      }
      if (!res.ok) {
        for (const x of form.querySelectorAll('button')) x.disabled = false;
        showUndoToast({title: t.iFailed, detail: res.message, undoLabel: t.iDismiss, onUndo: (el) => el.remove()});
        return;
      }
      row.classList.add('is-leaving');
      setTimeout(() => row.remove(), 180);
      bump(root, prev, -1);
      bump(root, next, 1);
      showUndoToast({
        title: fill(t.iSet, stateName(t, next)),
        detail: fill(t.iLeaves, where, stateName(t, prev)),
        undoLabel: t.iUndo,
        onUndo: async (toast) => {
          const back = new FormData(form);
          back.set('state', prev);
          back.set('note', '');
          back.set('op', uuidv7());
          let r;
          try {
            r = await post(form.action, back);
          } catch {
            r = {ok: false, message: ''};
          }
          if (r.ok) {
            window.location.reload();
          } else {
            toast.querySelector('b').textContent = t.iFailed;
            toast.querySelector('span').textContent = r.message;
          }
        },
      });
    });
  }
}

export function initAscSecurity() {
  const root = document.querySelector('[data-testid="asc-security"]');
  if (!root) return;
  initScan(root);
  initFindings(root);
}
