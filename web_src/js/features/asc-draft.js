// "Draft from the diff" of a new change (board NewChange): a button with data-asc-draft and data-url fetches the draft that
// asc writes from base..head (the subjects, the files, the size, the risk) and puts it into the title and the description.
// With data-summary-url the button first asks for a summary written by a model with the repository's AI key (What, Why,
// How to check, Risk and where to look). When there is no key, AI is off or summaries are paused, the facts draft is used
// and the note says why. Nothing is replaced without asking when the person has written something already.
//
// The brief of a stacked change (templates/repo/pulls/asc_brief.tmpl) has a button with data-asc-summary that fills the
// fields of the brief form the same way.
import {GET, POST} from '../modules/fetch.js';

export function applyDraft(draft, fields, confirmFn) {
  const {title, body} = fields;
  if (draft.nothing) return 'nothing';
  const written = (title?.value || '').trim() !== '' || (body?.value || '').trim() !== '';
  if (written && !confirmFn()) return 'kept';
  if (title && draft.title) title.value = draft.title;
  if (body) {
    body.value = draft.body || '';
    body.dispatchEvent(new Event('input', {bubbles: true}));
  }
  title?.dispatchEvent(new Event('input', {bubbles: true}));
  return 'applied';
}

const briefFields = [['what', 'what'], ['why', 'why'], ['how', 'how'], ['risk_note', 'risk'], ['where', 'where']];

// applySummary puts a summary into the fields of the brief form, keyed by the form names what, why, how, risk_note
// and where.
export function applySummary(summary, fields, confirmFn) {
  if (summary.nothing) return 'nothing';
  const written = briefFields.some(([name]) => (fields[name]?.value || '').trim() !== '');
  if (written && !confirmFn()) return 'kept';
  for (const [name, key] of briefFields) {
    const field = fields[name];
    if (!field) continue;
    field.value = summary[key] || '';
    field.dispatchEvent(new Event('input', {bubbles: true}));
  }
  return 'applied';
}

// newOpID makes a UUID version 7, the op id asc keys a mutating call by.
export function newOpID() {
  const b = crypto.getRandomValues(new Uint8Array(16));
  const ms = Date.now();
  for (let i = 0; i < 6; i++) b[i] = Math.floor(ms / 2 ** (8 * (5 - i))) % 256;
  b[6] = (b[6] & 0x0f) | 0x70;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

// askSummary posts for a summary. It answers {summary} when one was written, else {refusal} with the reason and its
// sentence. The op id of the next click is kept on the button: a double click is one call, a retry is a new one.
export async function askSummary(btn, data) {
  if (!btn.getAttribute('data-op')) btn.setAttribute('data-op', newOpID());
  data.set('op', btn.getAttribute('data-op'));
  let resp;
  try {
    resp = await POST(btn.getAttribute('data-summary-url'), {data, headers: {'x-csrf-token': window.config?.csrfToken ?? '', Accept: 'application/json'}});
  } catch {
    return {refusal: {error: 'unavailable', message: ''}};
  }
  let body = null;
  try {
    body = await resp.json();
  } catch {
    body = null;
  }
  if (body?.next) btn.setAttribute('data-op', body.next);
  if (resp.ok && body) return {summary: body};
  return {refusal: {error: body?.error || 'unavailable', message: body?.message || ''}};
}

async function factsDraft(btn) {
  const resp = await GET(btn.getAttribute('data-url'), {headers: {Accept: 'application/json'}});
  if (!resp.ok) throw new Error(String(resp.status));
  return resp.json();
}

function say(note, text) {
  if (note) note.textContent = text;
}

function initNewChange(btn) {
  const note = btn.parentElement?.querySelector('[data-asc-draft-note]');
  btn.addEventListener('click', async () => {
    btn.disabled = true;
    say(note, '');
    const form = btn.closest('form');
    const fields = {title: form?.querySelector('[name=title]'), body: form?.querySelector('[name=content]')};
    const confirmFn = () => window.confirm(btn.getAttribute('data-confirm') || '');
    try {
      let why = '';
      if (btn.hasAttribute('data-summary-url')) {
        say(note, btn.getAttribute('data-working') || '');
        const data = new URLSearchParams();
        data.set('base', btn.getAttribute('data-base') || '');
        data.set('head', btn.getAttribute('data-head') || '');
        const {summary, refusal} = await askSummary(btn, data);
        if (summary) {
          const result = applyDraft({nothing: summary.nothing, body: summary.body}, fields, confirmFn);
          say(note, result === 'nothing' ? btn.getAttribute('data-empty') || '' : result === 'applied' ? btn.getAttribute('data-written') || '' : '');
          return;
        }
        if (refusal.error === 'unknown') {
          say(note, refusal.message);
          return;
        }
        why = refusal.message;
      }
      const result = applyDraft(await factsDraft(btn), fields, confirmFn);
      if (result === 'nothing') {
        say(note, btn.getAttribute('data-empty') || '');
      } else {
        say(note, why ? `${why} ${btn.getAttribute('data-facts') || ''}`.trim() : '');
      }
    } catch {
      say(note, btn.getAttribute('data-failed') || '');
    } finally {
      btn.disabled = false;
    }
  });
}

function initBrief(btn) {
  const note = btn.parentElement?.querySelector('[data-asc-draft-note]');
  btn.addEventListener('click', async () => {
    btn.disabled = true;
    say(note, btn.getAttribute('data-working') || '');
    const form = btn.closest('form');
    const fields = Object.fromEntries(briefFields.map(([name]) => [name, form?.querySelector(`[name=${name}]`)]));
    try {
      const {summary, refusal} = await askSummary(btn, new URLSearchParams());
      if (!summary) {
        say(note, refusal.message || btn.getAttribute('data-failed') || '');
        return;
      }
      const result = applySummary(summary, fields, () => window.confirm(btn.getAttribute('data-confirm') || ''));
      say(note, result === 'nothing' ? btn.getAttribute('data-empty') || '' : result === 'applied' ? btn.getAttribute('data-written') || '' : '');
    } finally {
      btn.disabled = false;
    }
  });
}

export function initAscDraft() {
  for (const btn of document.querySelectorAll('[data-asc-draft]')) initNewChange(btn);
  for (const btn of document.querySelectorAll('[data-asc-summary]')) initBrief(btn);
}
