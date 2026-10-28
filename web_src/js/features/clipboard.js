import {showTemporaryTooltip} from '../modules/tippy.js';
import {toAbsoluteUrl} from '../utils.js';
import {clippie} from 'clippie';
import {showDsToast} from './ds-toast.js';

const {copy_success, copy_error} = window.config.i18n;

// Enable clipboard copy from HTML attributes. These properties are supported:
// - data-clipboard-text: Direct text to copy
// - data-clipboard-target: Holds a selector for a <input> or <textarea> whose content is copied
// - data-ds-toast-copied: A message shown in a toast after a good copy, in place of the tooltip
// - data-clipboard-text-type: When set to 'url' will convert relative to absolute urls
export function initGlobalCopyToClipboardListener() {
  document.addEventListener('click', async (e) => {
    const target = e.target.closest('[data-clipboard-text], [data-clipboard-target]');
    if (!target) return;

    e.preventDefault();

    let text = target.getAttribute('data-clipboard-text');
    if (!text) {
      text = document.querySelector(target.getAttribute('data-clipboard-target'))?.value;
    }

    if (text && target.getAttribute('data-clipboard-text-type') === 'url') {
      text = toAbsoluteUrl(text);
    }

    if (text) {
      const success = await clippie(text);
      // a control that names its own message (data-ds-toast-copied, for example "SHA-256 copied") answers with the toast of the design system
      const toastMessage = target.getAttribute('data-ds-toast-copied');
      if (toastMessage && success) {
        showDsToast(toastMessage);
      } else {
        showTemporaryTooltip(target, success ? copy_success : copy_error);
      }
    }
  });
}
