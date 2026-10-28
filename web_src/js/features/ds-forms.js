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

// List and Grid: <div data-layout-toggle data-layout-target=".css-selector" hidden> with two buttons
// data-layout-set="list|grid". The target list gets the class is-grid; the choice is kept in this browser and
// shared by every directory page. The toggle stays hidden without script (the list is the only layout then).
const layoutKey = 'asc-layout';

export function readLayout() {
  try {
    return window.localStorage.getItem(layoutKey) === 'grid' ? 'grid' : 'list';
  } catch {
    return 'list';
  }
}

export function initLayoutToggle() {
  for (const toggle of document.querySelectorAll('[data-layout-toggle]')) {
    const apply = (layout) => {
      for (const target of document.querySelectorAll(toggle.getAttribute('data-layout-target') || '')) target.classList.toggle('is-grid', layout === 'grid');
      for (const btn of toggle.querySelectorAll('[data-layout-set]')) btn.setAttribute('aria-pressed', String(btn.getAttribute('data-layout-set') === layout));
    };
    toggle.hidden = false;
    apply(readLayout());
    toggle.addEventListener('click', (e) => {
      const btn = e.target instanceof Element ? e.target.closest('[data-layout-set]') : null;
      if (!btn) return;
      const layout = btn.getAttribute('data-layout-set') === 'grid' ? 'grid' : 'list';
      try {
        window.localStorage.setItem(layoutKey, layout);
      } catch {
        // storage is blocked: the choice holds for this page only
      }
      apply(layout);
    });
  }
}

// A word for the strength of a new password: "short", "weak", "fair" or "strong". It is advice only; the server
// applies the real password rules. Length counts most, then how many kinds of characters are used.
export function passwordStrength(value, minLength = 8) {
  if (value.length < minLength) return 'short';
  const kinds = [/[a-z]/, /[A-Z]/, /\d/, /[^A-Za-z0-9]/].filter((re) => re.test(value)).length;
  const score = (value.length >= 16 ? 2 : value.length >= 12 ? 1 : 0) + (kinds >= 3 ? 1 : 0) + (kinds === 4 ? 1 : 0);
  if (score >= 3) return 'strong';
  return score >= 1 ? 'fair' : 'weak';
}

// <input data-password-strength data-min="8"> and <span data-strength-for="<input id>" data-words='{"short":"...","weak":"...","fair":"...","strong":"..."}'>:
// the word appears 300 ms after the person stops typing, in a polite live region.
export function initPasswordStrength() {
  for (const input of document.querySelectorAll('input[data-password-strength]')) {
    const out = document.querySelector(`[data-strength-for="${input.id}"]`);
    if (!out) continue;
    let words = {};
    try {
      words = JSON.parse(out.getAttribute('data-words') || '{}');
    } catch {
      words = {};
    }
    let timer = 0;
    input.addEventListener('input', () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(() => {
        if (input.value === '') {
          out.textContent = '';
          out.removeAttribute('data-level');
          return;
        }
        const level = passwordStrength(input.value, Number(input.getAttribute('data-min')) || 8);
        out.textContent = words[level] || '';
        out.setAttribute('data-level', level);
      }, 300);
    });
  }
}
