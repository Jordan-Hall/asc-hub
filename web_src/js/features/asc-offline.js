// Registers the service worker of the hub, which shows the offline page (and keeps its style) when the hub cannot be
// reached. It is skipped where service workers are not allowed: an insecure page, or a browser without them.

export function canRegisterWorker(win = window) {
  return Boolean(win.navigator && 'serviceWorker' in win.navigator && win.isSecureContext);
}

export function workerUrl(appSubUrl = '') {
  return {script: `${appSubUrl}/-/asc-sw.js`, scope: `${appSubUrl}/`};
}

export async function initAscOffline() {
  if (!canRegisterWorker()) return;
  const {script, scope} = workerUrl(window.config?.appSubUrl ?? '');
  try {
    await navigator.serviceWorker.register(script, {scope});
  } catch {
    // no worker: the browser shows its own page when the hub is away
  }
}
