import {POST} from '../../modules/fetch.js';
import {toggleElem} from '../../utils/dom.js';

export function initCompWebHookEditor() {
  if (!document.querySelectorAll('.new.webhook').length) {
    return;
  }

  // a generic hook sends a GET without a body, so its content type is shown for a POST only
  const httpMethodInput = document.getElementById('http_method');
  const contentTypeField = document.getElementById('content_type')?.closest('.ds-field');
  if (httpMethodInput && contentTypeField) {
    const updateContentType = () => toggleElem(contentTypeField, httpMethodInput.value === 'POST');
    updateContentType();
    httpMethodInput.addEventListener('change', updateContentType);
  }

  // Test delivery
  document.getElementById('test-delivery')?.addEventListener('click', async function () {
    this.classList.add('is-loading', 'disabled');
    await POST(this.getAttribute('data-link'));
    setTimeout(() => {
      window.location.href = this.getAttribute('data-redirect');
    }, 5000);
  });
}
