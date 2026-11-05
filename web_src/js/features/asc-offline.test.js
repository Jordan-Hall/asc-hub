import {canRegisterWorker, workerUrl, initAscOfflinePage} from './asc-offline.js';
import {draftKey, packDraft} from './asc-drafts.js';

test('the worker is registered only where the browser allows it', () => {
  expect(canRegisterWorker({navigator: {serviceWorker: {}}, isSecureContext: true})).toBe(true);
  expect(canRegisterWorker({navigator: {serviceWorker: {}}, isSecureContext: false})).toBe(false);
  expect(canRegisterWorker({navigator: {}, isSecureContext: true})).toBe(false);
});

test('the worker and its scope follow the sub-path of the site', () => {
  expect(workerUrl()).toEqual({script: '/-/asc-sw.js', scope: '/'});
  expect(workerUrl('/hub')).toEqual({script: '/hub/-/asc-sw.js', scope: '/hub/'});
});

test('the offline page says the edit is saved on this device when a draft is kept', () => {
  const offline = '<p data-asc-offline-text>The hub cannot be reached.</p><p data-asc-offline-draft hidden>Your edit is saved on this device.</p>';
  document.body.innerHTML = offline;
  const store = new Map();
  const storage = {key: (i) => Array.from(store.keys())[i], get length() { return store.size }};
  initAscOfflinePage(document, storage);
  expect(document.querySelector('[data-asc-offline-draft]').hidden).toBe(true);

  store.set(draftKey('/a/b/issues/3', 'content'), packDraft('x'));
  initAscOfflinePage(document, storage);
  expect(document.querySelector('[data-asc-offline-draft]').hidden).toBe(false);
  expect(document.querySelector('[data-asc-offline-text]').hidden).toBe(true);
});
