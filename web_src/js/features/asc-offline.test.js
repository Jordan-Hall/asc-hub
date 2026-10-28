import {canRegisterWorker, workerUrl} from './asc-offline.js';

test('the worker is registered only where the browser allows it', () => {
  expect(canRegisterWorker({navigator: {serviceWorker: {}}, isSecureContext: true})).toBe(true);
  expect(canRegisterWorker({navigator: {serviceWorker: {}}, isSecureContext: false})).toBe(false);
  expect(canRegisterWorker({navigator: {}, isSecureContext: true})).toBe(false);
});

test('the worker and its scope follow the sub-path of the site', () => {
  expect(workerUrl()).toEqual({script: '/-/asc-sw.js', scope: '/'});
  expect(workerUrl('/hub')).toEqual({script: '/hub/-/asc-sw.js', scope: '/hub/'});
});
