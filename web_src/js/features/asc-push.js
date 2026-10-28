import {POST} from '../modules/fetch.js';

// Notifications in the browser (Web Push) on the Notifications settings page. A block with data-asc-push holds the
// public key of the hub (data-vapid), the service worker (data-sw, data-scope), the two addresses (data-subscribe-url,
// data-unsubscribe-url), the texts of the states (data-t-*), a status line [data-push-status] and the buttons
// [data-push-enable] and [data-push-disable]. Nothing is asked of the browser until the person presses the button.

export function urlBase64ToUint8Array(value) {
  const pad = '='.repeat((4 - (value.length % 4)) % 4);
  const raw = atob((value + pad).replaceAll('-', '+').replaceAll('_', '/'));
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

export function pushSupported(win = window) {
  return Boolean(win.navigator && 'serviceWorker' in win.navigator && 'PushManager' in win && 'Notification' in win);
}

// subscriptionBody is what the hub stores: the address and the two keys of the browser, or null when one is missing.
export function subscriptionBody(subscription) {
  const json = subscription && typeof subscription.toJSON === 'function' ? subscription.toJSON() : null;
  if (!json || typeof json.endpoint !== 'string' || !json.keys?.p256dh || !json.keys?.auth) return null;
  return {endpoint: json.endpoint, keys: {p256dh: json.keys.p256dh, auth: json.keys.auth}};
}

function post(url, body) {
  return POST(url, {data: body, headers: {'x-csrf-token': window.config?.csrfToken ?? ''}});
}

function show(root, state) {
  const status = root.querySelector('[data-push-status]');
  if (status) status.textContent = root.getAttribute(`data-t-${state}`) ?? '';
  root.setAttribute('data-push-state', state);
  const enable = root.querySelector('[data-push-enable]');
  const disable = root.querySelector('[data-push-disable]');
  if (enable) enable.hidden = state !== 'off';
  if (disable) disable.hidden = state !== 'on';
}

async function currentSubscription(root) {
  const registration = await navigator.serviceWorker.getRegistration(root.getAttribute('data-scope') || '/');
  return registration ? registration.pushManager.getSubscription() : null;
}

export async function refreshPushState(root) {
  if (!pushSupported()) return show(root, 'unsupported');
  if (Notification.permission === 'denied') return show(root, 'denied');
  try {
    show(root, (await currentSubscription(root)) ? 'on' : 'off');
  } catch {
    show(root, 'off');
  }
}

async function enable(root) {
  try {
    if ((await Notification.requestPermission()) !== 'granted') return show(root, 'denied');
    const registration = await navigator.serviceWorker.register(root.getAttribute('data-sw'), {scope: root.getAttribute('data-scope') || '/'});
    await navigator.serviceWorker.ready;
    let subscription = await registration.pushManager.getSubscription();
    subscription ??= await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(root.getAttribute('data-vapid')),
    });
    const body = subscriptionBody(subscription);
    if (!body) throw new Error('incomplete subscription');
    const res = await post(root.getAttribute('data-subscribe-url'), body);
    if (!res.ok) {
      await subscription.unsubscribe();
      throw new Error(`hub answered ${res.status}`);
    }
    show(root, 'on');
  } catch {
    show(root, 'error');
  }
}

async function disable(root) {
  try {
    const subscription = await currentSubscription(root);
    if (subscription) {
      const body = subscriptionBody(subscription);
      await subscription.unsubscribe();
      if (body) await post(root.getAttribute('data-unsubscribe-url'), {endpoint: body.endpoint});
    }
    show(root, 'off');
  } catch {
    show(root, 'error');
  }
}

export function initAscPush() {
  for (const root of document.querySelectorAll('[data-asc-push]')) {
    root.querySelector('[data-push-enable]')?.addEventListener('click', () => enable(root));
    root.querySelector('[data-push-disable]')?.addEventListener('click', () => disable(root));
    refreshPushState(root);
  }
}
