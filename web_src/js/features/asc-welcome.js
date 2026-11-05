// Get started (board GetStarted): while step 2 waits for the first sign-in of the command line, the checklist asks the
// hub every few seconds whether it has arrived. When it has, the page fetches itself again and puts the new steps,
// progress and health card in place, so step 2 ticks and step 3 becomes the current one without a reload. A screen
// reader hears it from the status line of the page.
//
// [data-gs-watch]: the status URL (GET /-/get-started/status, JSON {connected, done, total}). [data-gs-part]: a part
// of the page that is swapped. [data-gs-live]: the status line, data-t-connected its words.
//
// Step 3 imports in place: [data-gs-imp-open] (the "Import an existing project" link, which goes to /repo/migrate
// without scripts) opens the form [data-gs-imp]. It posts the address (POST /-/get-started/import, inline=1) and then
// asks data-status?repo=name every IMPORT_INTERVAL until the task ends: "Reading history" with a running bar, then the
// number of commits read and a link to the repository. data-t-done and data-t-lost are the words of the end and of a
// hub that stopped answering.

import {GET, POST} from '../modules/fetch.js';

export const WATCH_INTERVAL = 3000;
export const IMPORT_INTERVAL = 1500;
const IMPORT_TRIES = 3; // failed answers in a row before the page gives up asking

// swapParts puts each [data-gs-part] of next in place of the part of root with the same name.
export function swapParts(root, next) {
  for (const part of next.querySelectorAll('[data-gs-part]')) {
    const name = part.getAttribute('data-gs-part');
    const old = root.querySelector(`[data-gs-part="${name}"]`);
    if (old) old.replaceWith(document.importNode(part, true));
  }
}

async function isConnected(url) {
  const resp = await GET(url);
  if (!resp.ok) return false;
  return Boolean((await resp.json())?.connected);
}

// refreshParts fetches the page again and swaps its parts; names keeps the swap to those parts.
export async function refreshParts(root, names = null) {
  const resp = await GET(window.location.href);
  if (!resp.ok) return false;
  const next = new DOMParser().parseFromString(await resp.text(), 'text/html');
  if (names) {
    for (const part of next.querySelectorAll('[data-gs-part]')) {
      if (!names.includes(part.getAttribute('data-gs-part'))) part.remove();
    }
  }
  swapParts(root, next);
  return true;
}

async function refresh(root) {
  if (!await refreshParts(root)) return false;
  root.removeAttribute('data-gs-watch');
  initImport(root); // the steps are new, so is the form of step 3
  const live = root.querySelector('[data-gs-live]');
  if (live) live.textContent = live.getAttribute('data-t-connected') ?? '';
  return true;
}

// watch asks url until the command line has signed in, then refreshes the checklist once. A hidden tab does not ask;
// a failed answer is asked again on the next turn.
export function watch(root, url, interval = WATCH_INTERVAL) {
  const turn = async () => {
    let done = false;
    if (!document.hidden) {
      try {
        done = await isConnected(url) && await refresh(root);
      } catch {
        done = false;
      }
    }
    if (!done) setTimeout(turn, interval);
  };
  setTimeout(turn, interval);
}

const show = (el, on) => el?.classList.toggle('tw-hidden', !on);

// importParts are the parts of the form [data-gs-imp].
function importParts(form) {
  const one = (name) => form.querySelector(`[data-gs-imp-${name}]`);
  return {
    input: form.querySelector('[name=clone_addr]'),
    err: one('err'), run: one('run'), count: one('count'), bar: one('bar'), live: one('live'),
    go: one('go'), back: one('back'), repo: one('repo'),
  };
}

function showImportError(p, text) {
  p.err.textContent = text;
  show(p.err, Boolean(text));
  p.input.setAttribute('aria-invalid', text ? 'true' : 'false');
}

function setRunning(p, running) {
  p.go.disabled = running;
  p.input.readOnly = running;
}

// finishImport shows the end of an import: the commits read, the link to the repository and the new progress.
function finishImport(root, form, p, st) {
  p.count.textContent = st.count ?? '';
  p.bar.value = 1;
  p.live.textContent = form.getAttribute('data-t-done') ?? '';
  p.repo.href = st.link;
  show(p.go, false);
  show(p.back, false);
  show(p.repo, true);
  refreshParts(root, ['top', 'side', 'health']).catch(() => {});
}

// followImport asks how the import of repo ({name, link}) goes until it ends.
export function followImport(root, form, repo, interval = IMPORT_INTERVAL) {
  const p = importParts(form);
  const url = `${form.getAttribute('data-status')}?repo=${encodeURIComponent(repo.name)}`;
  let misses = 0;
  const turn = async () => {
    let st = null;
    try {
      const resp = await GET(url);
      if (resp.ok) st = await resp.json();
    } catch {
      st = null;
    }
    if (!st) {
      misses++;
      if (misses < IMPORT_TRIES) return setTimeout(turn, interval);
      showImportError(p, form.getAttribute('data-t-lost') ?? '');
      p.repo.href = repo.link;
      show(p.repo, true);
      return;
    }
    misses = 0;
    if (st.status === 'done') return finishImport(root, form, p, st);
    if (st.status === 'failed') {
      show(p.run, false);
      setRunning(p, false);
      return showImportError(p, st.message ?? '');
    }
    setTimeout(turn, interval);
  };
  setTimeout(turn, interval);
}

async function startImport(root, form, interval) {
  const p = importParts(form);
  showImportError(p, '');
  setRunning(p, true);
  const data = new FormData(form);
  data.set('inline', '1');
  let resp;
  try {
    resp = await POST(form.getAttribute('action'), {data});
  } catch {
    resp = null;
  }
  const reply = resp ? await resp.json().catch(() => ({})) : {};
  if (!resp?.ok) {
    setRunning(p, false);
    return showImportError(p, reply.error ?? form.getAttribute('data-t-lost') ?? '');
  }
  p.count.textContent = '';
  p.bar.removeAttribute('value');
  show(p.run, true);
  followImport(root, form, {name: reply.repo, link: reply.link}, interval);
}

// initImport opens the form in place of the choice of step 3, and Back puts the choice back.
export function initImport(root, interval = IMPORT_INTERVAL) {
  const form = root.querySelector('[data-gs-imp]');
  const open = root.querySelector('[data-gs-imp-open]');
  if (!form || !open) return;
  const choose = root.querySelector('[data-gs-choose]');
  const heading = root.querySelector('[data-gs-imp-h]');
  const before = heading?.innerHTML;
  const setOpen = (on) => {
    show(choose, !on);
    show(form, on);
    if (heading) {
      if (on) heading.textContent = heading.getAttribute('data-gs-imp-h');
      else heading.innerHTML = before;
    }
  };
  open.addEventListener('click', (e) => {
    e.preventDefault();
    setOpen(true);
    form.querySelector('[name=clone_addr]').focus();
  });
  form.querySelector('[data-gs-imp-back]').addEventListener('click', () => {
    setOpen(false);
    open.focus();
  });
  form.addEventListener('submit', (e) => {
    e.preventDefault();
    startImport(root, form, interval);
  });
}

export function initAscWelcome() {
  const page = document.querySelector('.site-started');
  if (page) initImport(page);
  const root = document.querySelector('[data-gs-watch]');
  if (root) watch(root, root.getAttribute('data-gs-watch'));
}
