// Registers the service worker of the hub, which shows the offline page (and keeps its style) when the hub cannot be
// reached. It is skipped where service workers are not allowed: an insecure page, or a browser without them.

import {hasDrafts} from './asc-drafts.js';

export function canRegisterWorker(win = window) {
  return Boolean(win.navigator && 'serviceWorker' in win.navigator && win.isSecureContext);
}

export function workerUrl(appSubUrl = '') {
  return {script: `${appSubUrl}/-/asc-sw.js`, scope: `${appSubUrl}/`};
}

/** The offline page (templates/status/offline.tmpl): when a draft is kept, it says the edit is saved on this device. */
export function initAscOfflinePage(doc = document, storage = window.localStorage) {
  const draft = doc.querySelector('[data-asc-offline-draft]');
  if (!draft || !hasDrafts(storage)) return;
  draft.hidden = false;
  doc.querySelector('[data-asc-offline-text]')?.setAttribute('hidden', '');
}

export async function initAscOffline() {
  initAscOfflinePage();
  if (!canRegisterWorker()) return;
  const {script, scope} = workerUrl(window.config?.appSubUrl ?? '');
  try {
    await navigator.serviceWorker.register(script, {scope});
  } catch {
    // no worker: the browser shows its own page when the hub is away
  }
}
