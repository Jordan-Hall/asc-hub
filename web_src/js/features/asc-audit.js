import {POST, GET} from '../modules/fetch.js';

// The events one slice of a verification reads, so the bar moves on a long log.
export const VERIFY_SLICE = 2000;

// verifySlices plans the slices of a log whose events run from firstSeq to headSeq. Only the last slice ends at
// 0 (the newest event), which also checks the head, so a removed tail is still found. A short log is one slice.
export function verifySlices(firstSeq, headSeq, size = VERIFY_SLICE) {
  if (!headSeq || headSeq < firstSeq || headSeq - firstSeq < size) return [[0, 0]];
  const slices = [];
  let from = firstSeq;
  while (headSeq - from >= size) {
    slices.push([from, from + size - 1]);
    from += size;
  }
  slices.push([from, 0]);
  return slices;
}

// exportFileName reads the file name of a Content-Disposition header.
export function exportFileName(disposition, fallback) {
  const star = /filename\*=UTF-8''([^;]+)/i.exec(disposition || '');
  if (star) {
    try {
      return decodeURIComponent(star[1].trim());
    } catch {
      // a bad escape falls through to the plain name
    }
  }
  const plain = /filename="((?:[^"\\]|\\.)*)"/i.exec(disposition || '');
  return plain ? plain[1].replace(/\\(.)/g, '$1') : fallback;
}

// formatSize writes a byte count the way the board does: 820 B, 8.2 kB, 8.2 MB.
export function formatSize(bytes, locale) {
  const units = ['B', 'kB', 'MB', 'GB'];
  let n = bytes;
  let u = 0;
  while (n >= 1000 && u < units.length - 1) {
    n /= 1000;
    u++;
  }
  const digits = u === 0 || n >= 100 ? 0 : 1;
  return `${n.toLocaleString(locale, {maximumFractionDigits: digits, minimumFractionDigits: digits})} ${units[u]}`;
}

function fill(text, values) {
  return text.replace(/\{(\w+)\}/g, (m, k) => (k in values ? String(values[k]) : m));
}

async function verifyStep(form, from, to) {
  const data = new FormData(form);
  data.set('step', '1');
  data.set('from', String(from));
  data.set('to', String(to));
  const res = await POST(form.action, {data, headers: {Accept: 'application/json'}});
  if (!res.ok) throw new Error(`verify step: ${res.status}`);
  return res.json();
}

function submitSlice(form, from, to, done) {
  form.elements.namedItem('from').value = from ? String(from) : '';
  form.elements.namedItem('to').value = to ? String(to) : '';
  form.elements.namedItem('done').value = done ? String(done) : '';
  form.submit();
}

function initVerify(form) {
  const progress = document.querySelector('[data-asc-audit-progress]');
  if (!progress) return;
  const bar = progress.querySelector('[role="progressbar"]');
  const text = progress.querySelector('[data-asc-audit-progress-text]');
  const locale = document.documentElement.lang || undefined;
  const show = (done, total) => {
    const pct = total ? Math.min(100, Math.floor((done * 100) / total)) : 0;
    bar.setAttribute('aria-valuenow', String(pct));
    bar.querySelector('i').style.width = `${pct}%`;
    text.textContent = fill(form.getAttribute('data-progress-text'), {done: done.toLocaleString(locale), total: total.toLocaleString(locale)});
  };
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const button = form.querySelector('button[type="submit"]');
    button.disabled = true;
    try {
      // A slice of one event learns where the log starts and ends.
      const probe = await verifyStep(form, 1, 1);
      const slices = verifySlices(probe.firstSeq, probe.headSeq);
      if (slices.length === 1) {
        submitSlice(form, 0, 0, 0);
        return;
      }
      const total = probe.headSeq - probe.firstSeq + 1;
      let done = 0;
      progress.hidden = false;
      show(0, total);
      for (const [from, to] of slices.slice(0, -1)) {
        const step = await verifyStep(form, from, to);
        if (!step.ok) {
          // The page names the broken event: verify that slice again as a plain post.
          submitSlice(form, from, to, done);
          return;
        }
        done += step.checked;
        show(done, total);
      }
      const [from] = slices.at(-1);
      submitSlice(form, from, 0, done);
    } catch {
      // Without the slices the server verifies the whole log in one post.
      submitSlice(form, 0, 0, 0);
    }
  });
}

// showExportBusy shows that an export is being prepared: the Export button spins and the status line, which a
// screen reader announces, says what happens next.
export function showExportBusy(button, status, busy) {
  button?.classList.toggle('is-loading', busy);
  if (status) status.textContent = busy ? status.getAttribute('data-text') : '';
}

function initExports(card) {
  const sub = card.querySelector('[data-asc-audit-ready-sub]');
  const link = card.querySelector('[data-asc-audit-ready-link]');
  const locale = document.documentElement.lang || undefined;
  let url = '';
  link.addEventListener('click', () => {
    // The browser starts the download first; the card goes after it.
    setTimeout(() => {
      card.hidden = true;
    }, 0);
  });
  for (const a of document.querySelectorAll('a[data-asc-audit-export]')) {
    a.addEventListener('click', async (e) => {
      if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
      e.preventDefault();
      a.setAttribute('aria-busy', 'true');
      const menu = a.closest('details');
      const button = menu?.querySelector('summary');
      const status = document.querySelector('[data-asc-audit-export-status]');
      showExportBusy(button, status, true);
      if (menu) menu.open = false;
      try {
        const res = await GET(a.href);
        if (!res.ok) throw new Error(`export: ${res.status}`);
        const blob = await res.blob();
        const name = exportFileName(res.headers.get('Content-Disposition'), 'asc-audit');
        const events = Number(res.headers.get('X-Asc-Audit-Events') || '0');
        if (url) URL.revokeObjectURL(url);
        url = URL.createObjectURL(blob);
        link.href = url;
        link.setAttribute('download', name);
        const count = fill(card.getAttribute(events === 1 ? 'data-events-one' : 'data-events-other'), {n: events.toLocaleString(locale)});
        sub.textContent = `${name}, ${formatSize(blob.size, locale)}, ${count}.`;
        card.hidden = false;
        link.focus();
      } catch {
        // The plain link shows the error page of the server.
        window.location.href = a.href;
      } finally {
        a.removeAttribute('aria-busy');
        showExportBusy(button, status, false);
      }
    });
  }
}

export function initAscAudit() {
  const form = document.querySelector('form[data-asc-audit-verify]');
  if (form) initVerify(form);
  const card = document.querySelector('[data-asc-audit-ready]');
  if (card) initExports(card);
}
