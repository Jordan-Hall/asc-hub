// Get started (board GetStarted): while step 2 waits for the first sign-in of the command line, the checklist asks the
// hub every few seconds whether it has arrived. When it has, the page fetches itself again and puts the new steps,
// progress and health card in place, so step 2 ticks and step 3 becomes the current one without a reload. A screen
// reader hears it from the status line of the page.
//
// [data-gs-watch]: the status URL (GET /-/get-started/status, JSON {connected, done, total}). [data-gs-part]: a part
// of the page that is swapped. [data-gs-live]: the status line, data-t-connected its words.

import {GET} from '../modules/fetch.js';

export const WATCH_INTERVAL = 3000;

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

async function refresh(root) {
  const resp = await GET(window.location.href);
  if (!resp.ok) return false;
  const next = new DOMParser().parseFromString(await resp.text(), 'text/html');
  swapParts(root, next);
  root.removeAttribute('data-gs-watch');
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

export function initAscWelcome() {
  const root = document.querySelector('[data-gs-watch]');
  if (root) watch(root, root.getAttribute('data-gs-watch'));
}
