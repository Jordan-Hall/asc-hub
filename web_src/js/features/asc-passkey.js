// Sign in with a passkey (board Onboarding): the button under the sign in form asks the hub for a challenge that names
// no account, lets the browser offer the passkeys it holds for the hub, and sends the signed answer back. The page does
// not change until the hub has signed the person in. Errors show in the security key banner of the page
// (templates/user/auth/webauthn_error.tmpl).
//
// [data-asc-passkey]: data-remember (selector of the "keep me signed in" box).

import {encodeURLEncodedBase64, decodeURLEncodedBase64} from '../utils.js';
import {showElem} from '../utils/dom.js';
import {GET, POST} from '../modules/fetch.js';

// passkeyOptions turns the JSON of the hub into the options of navigator.credentials.get.
export function passkeyOptions(json) {
  const publicKey = {...json.publicKey, challenge: decodeURLEncodedBase64(json.publicKey.challenge)};
  publicKey.allowCredentials = (json.publicKey.allowCredentials ?? []).map((c) => ({...c, id: decodeURLEncodedBase64(c.id)}));
  return {publicKey, mediation: json.mediation || undefined};
}

const b64 = (buf) => encodeURLEncodedBase64(new Uint8Array(buf));

// passkeyBody is the answer of the browser as the hub reads it.
export function passkeyBody(cred) {
  return {
    id: cred.id,
    rawId: b64(cred.rawId),
    type: cred.type,
    clientExtensionResults: cred.getClientExtensionResults(),
    response: {
      authenticatorData: b64(cred.response.authenticatorData),
      clientDataJSON: b64(cred.response.clientDataJSON),
      signature: b64(cred.response.signature),
      userHandle: b64(cred.response.userHandle ?? new ArrayBuffer(0)),
    },
  };
}

function showError(kind, message) {
  const msg = document.getElementById('webauthn-error-msg');
  if (!msg) return;
  const typed = document.querySelector(`#webauthn-error [data-webauthn-error-msg=${kind}]`);
  msg.textContent = typed ? typed.textContent : message || kind;
  showElem('#webauthn-error');
}

async function signInWithPasskey(button) {
  const {appSubUrl} = window.config;
  if (!window.isSecureContext) return showError('insecure');
  if (typeof window.PublicKeyCredential !== 'function') return showError('browser');
  button.disabled = true;
  try {
    const res = await GET(`${appSubUrl}/user/webauthn/passkey/assertion`);
    if (!res.ok) return showError('unknown');
    let cred;
    try {
      cred = await navigator.credentials.get(passkeyOptions(await res.json()));
    } catch (err) {
      // the person closed the dialog of the browser: nothing to report
      if (err.name === 'NotAllowedError' || err.name === 'AbortError') return;
      return showError('general', err.message);
    }
    if (!cred) return showError('empty');
    const remember = document.querySelector(button.getAttribute('data-remember'))?.checked ? '?remember=on' : '';
    const done = await POST(`${appSubUrl}/user/webauthn/passkey/login${remember}`, {data: passkeyBody(cred)});
    if (!done.ok) return showError(done.status === 403 ? 'unable-to-process' : 'unknown');
    const reply = await done.json();
    window.location.href = reply?.redirect ?? `${appSubUrl}/`;
  } finally {
    button.disabled = false;
  }
}

export function initAscPasskey() {
  for (const button of document.querySelectorAll('[data-asc-passkey]')) {
    button.addEventListener('click', () => signInWithPasskey(button));
  }
}
