import {passkeyBody, passkeyOptions} from './asc-passkey.js';

const bytes = (...b) => new Uint8Array(b).buffer;

test('passkeyOptions decodes the challenge and names no credential', () => {
  const {publicKey} = passkeyOptions({publicKey: {challenge: 'AQID', rpId: 'example.org', userVerification: 'required'}});
  expect(Array.from(new Uint8Array(publicKey.challenge))).toEqual([1, 2, 3]);
  expect(publicKey.allowCredentials).toEqual([]);
  expect(publicKey.userVerification).toBe('required');
});

test('passkeyBody encodes the answer as the hub reads it', () => {
  const body = passkeyBody({
    id: 'BAU',
    rawId: bytes(4, 5),
    type: 'public-key',
    getClientExtensionResults: () => ({}),
    response: {authenticatorData: bytes(1), clientDataJSON: bytes(2), signature: bytes(3), userHandle: bytes(6)},
  });
  expect(body).toEqual({
    id: 'BAU',
    rawId: 'BAU',
    type: 'public-key',
    clientExtensionResults: {},
    response: {authenticatorData: 'AQ', clientDataJSON: 'Ag', signature: 'Aw', userHandle: 'Bg'},
  });
});
