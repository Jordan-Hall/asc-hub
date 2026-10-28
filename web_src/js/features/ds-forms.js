// Small behaviours of the settings forms of the ASC design system.
//
// Live preview: a container with data-preview-form="<form id>" holds elements with data-preview-field="<input
// name>"; each shows the text of that input as the person types. data-preview-fallback names a second input
// (full name falls back to the username) and data-preview-empty marks an element that is hidden while its text
// is empty. The preview only mirrors the form: nothing is sent.
//
// Image preview: <input type="file" data-image-preview="<img id>"> shows the chosen image in that <img> before
// the form is sent. A file that is not an image is left out and the old picture stays.

export function previewText(form, name, fallbackName = '') {
  const read = (n) => (n && form.elements[n] ? String(form.elements[n].value || '').trim() : '');
  return read(name) || read(fallbackName);
}

export function initLivePreview() {
  for (const box of document.querySelectorAll('[data-preview-form]')) {
    const form = document.getElementById(box.getAttribute('data-preview-form'));
    if (!form) continue;
    const update = () => {
      for (const el of box.querySelectorAll('[data-preview-field]')) {
        const text = previewText(form, el.getAttribute('data-preview-field'), el.getAttribute('data-preview-fallback') || '');
        el.textContent = text;
        if (el.hasAttribute('data-preview-empty')) el.hidden = text === '';
      }
    };
    form.addEventListener('input', update);
    update();
  }
}

export function initImagePreview() {
  for (const input of document.querySelectorAll('input[type="file"][data-image-preview]')) {
    const holder = document.getElementById(input.getAttribute('data-image-preview'));
    const target = holder && (holder.tagName === 'IMG' ? holder : holder.querySelector('img'));
    if (!target) continue;
    let url = '';
    input.addEventListener('change', () => {
      const file = input.files?.[0];
      if (!file || !file.type.startsWith('image/')) return;
      if (url) URL.revokeObjectURL(url);
      url = URL.createObjectURL(file);
      target.src = url;
      target.removeAttribute('srcset');
    });
  }
}
