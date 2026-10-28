import {urlBase64ToUint8Array, pushSupported, subscriptionBody, refreshPushState} from './asc-push.js';

test('urlBase64ToUint8Array reads the key of the hub with or without padding', () => {
  expect(Array.from(urlBase64ToUint8Array('AQID'))).toEqual([1, 2, 3]);
  expect(Array.from(urlBase64ToUint8Array('-_8'))).toEqual([251, 255]);
  expect(Array.from(urlBase64ToUint8Array('-_8='))).toEqual([251, 255]);
  expect(urlBase64ToUint8Array('')).toHaveLength(0);
});

test('subscriptionBody keeps the address and both keys and nothing else', () => {
  const sub = {toJSON: () => ({endpoint: 'https://p.example/x', expirationTime: null, keys: {p256dh: 'k', auth: 'a', extra: 'z'}})};
  expect(subscriptionBody(sub)).toEqual({endpoint: 'https://p.example/x', keys: {p256dh: 'k', auth: 'a'}});
  expect(subscriptionBody({toJSON: () => ({endpoint: 'https://p.example/x', keys: {p256dh: 'k'}})})).toBeNull();
  expect(subscriptionBody({toJSON: () => ({keys: {p256dh: 'k', auth: 'a'}})})).toBeNull();
  expect(subscriptionBody(null)).toBeNull();
  expect(subscriptionBody({})).toBeNull();
});

test('pushSupported needs a service worker, push and notifications', () => {
  expect(pushSupported({navigator: {serviceWorker: {}}, PushManager: {}, Notification: {}})).toBe(true);
  expect(pushSupported({navigator: {}, PushManager: {}, Notification: {}})).toBe(false);
  expect(pushSupported({navigator: {serviceWorker: {}}, Notification: {}})).toBe(false);
});

test('the block says when the browser cannot show notifications and hides both buttons wrongly shown', async () => {
  document.body.innerHTML = `<div data-asc-push data-t-unsupported="Not supported" data-t-on="On" data-t-off="Off">
    <p data-push-status></p><button data-push-enable>e</button><button data-push-disable hidden>d</button></div>`;
  const root = document.querySelector('[data-asc-push]');
  await refreshPushState(root);
  expect(root.querySelector('[data-push-status]').textContent).toBe('Not supported');
  expect(root.getAttribute('data-push-state')).toBe('unsupported');
  expect(root.querySelector('[data-push-enable]').hidden).toBe(true);
  expect(root.querySelector('[data-push-disable]').hidden).toBe(true);
});
