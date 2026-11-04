import {GET} from '../modules/fetch.js';

/** Shows the answer of the name check on its line. Only text is assigned. */
export function applyNameCheck(line, answer, input = document.getElementById('repo_name')) {
  if (!line) return;
  if (!answer) {
    line.hidden = true;
    return;
  }
  line.hidden = false;
  line.textContent = answer.ok ? (line.getAttribute('data-ok-text') || '') : String(answer.message ?? '');
  line.classList.toggle('is-err', !answer.ok);
  line.classList.toggle('is-ok', Boolean(answer.ok));
  input?.setAttribute('aria-invalid', answer.ok ? 'false' : 'true');
}

/**
 * Starts the visibility cards from the default of the owner chosen (an organization can set one), until the person
 * picks a visibility themselves.
 */
export function initOwnerVisibility(owner = document.getElementById('uid'), form = owner?.form) {
  if (!owner?.querySelector('option[data-default-visibility]') || !form) return;
  let touched = false;
  const cards = () => form.querySelectorAll('input[name="visibility"]');
  for (const card of cards()) {
    card.addEventListener('change', () => {
      touched = true;
    });
  }
  owner.addEventListener('change', () => {
    const want = owner.options[owner.selectedIndex]?.getAttribute('data-default-visibility');
    if (touched || !want) return;
    for (const card of cards()) {
      if (card.value === want && !card.disabled) card.checked = true;
    }
  });
}

/** What the preview shows, read from the form: owner and name, the visibility label and whether a policy file is added. */
export function previewOf(form) {
  const owner = form.querySelector('#uid');
  const vis = form.querySelector('input[name="visibility"]:checked');
  return {
    owner: owner?.options[owner.selectedIndex]?.getAttribute('data-name') || '',
    name: form.querySelector('#repo_name')?.value.trim() || '',
    visibility: vis?.closest('label')?.querySelector('strong')?.textContent.trim() || '',
    policy: Boolean(form.querySelector('input[name="policy_file"]')?.checked),
  };
}

/** Puts a preview into the card. Only text is assigned. */
export function applyPreview(card, p) {
  const name = p.name || card.querySelector('[data-pv-name]')?.getAttribute('data-noname') || '';
  const set = (sel, text) => {
    const el = card.querySelector(sel);
    if (el) el.textContent = text;
  };
  const url = `${card.querySelector('[data-pv-url]')?.getAttribute('data-base') || ''}${p.owner}/${name}.git`;
  set('[data-pv-name]', `${p.owner} / ${name}`);
  if (p.visibility) set('[data-pv-vis]', p.visibility);
  const policy = card.querySelector('[data-pv-policy]');
  if (policy) policy.hidden = !p.policy;
  set('[data-pv-url]', url);
  set('[data-pv-dir]', name);
  card.querySelector('[data-pv-copy]')?.setAttribute('data-clipboard-text', `asc clone ${url}\ncd ${name}`);
}

/** The text of the busy button: "Creating acme/ledger…", with "and its first policy" when a policy file is added. */
export function busyText(button, p) {
  const tpl = (p.policy && button.getAttribute('data-busy-policy-text')) || button.getAttribute('data-busy-text') || '';
  return tpl.replace('{repo}', `${p.owner}/${p.name}`);
}

/** Keeps the preview in step and shows the creating state on submit. The page coming back from history is reset. */
export function initNewRepoPreview(form = document.querySelector('.rp-new-form')) {
  if (!form) return;
  const card = form.querySelector('[data-new-preview]');
  const update = () => card && applyPreview(card, previewOf(form));
  form.addEventListener('input', update);
  form.addEventListener('change', update);
  update();
  const button = form.querySelector('[data-new-submit]');
  if (!button) return;
  const idle = button.textContent;
  form.addEventListener('submit', () => {
    button.textContent = busyText(button, previewOf(form));
    button.setAttribute('aria-busy', 'true');
    // disabled after the submit has started, so the button's own name and value are not what stops the post
    setTimeout(() => {
      button.disabled = true;
    }, 0);
  });
  window.addEventListener('pageshow', () => {
    button.textContent = idle;
    button.removeAttribute('aria-busy');
    button.disabled = false;
  });
}

/** An importer's link with what was typed: owner, name, description and whether it is private. */
export function importHref(href, form) {
  const url = new URL(href, window.location.href);
  const q = url.searchParams;
  for (const key of ['org', 'repo_name', 'description', 'private']) q.delete(key);
  const owner = form.querySelector('#uid')?.value;
  const name = form.querySelector('#repo_name')?.value.trim();
  const description = form.querySelector('#repo_desc')?.value.trim();
  const vis = form.querySelector('input[name="visibility"]:checked')?.value;
  if (owner) q.set('org', owner);
  if (name) q.set('repo_name', name);
  if (description) q.set('description', description);
  if (vis) q.set('private', vis === 'private' ? '1' : '0');
  return `${url.pathname}${url.search}`;
}

/** Empties the template choice so the repository is created empty. */
function clearTemplate(form) {
  const input = form.querySelector('#repo_template');
  if (!input?.value) return;
  input.value = '';
  input.dispatchEvent(new Event('change', {bubbles: true}));
  const search = form.querySelector('#repo_template_search');
  const text = search?.querySelector(':scope > .text');
  if (text) {
    text.textContent = search.getAttribute('data-placeholder') || '';
    text.classList.add('default');
  }
}

/** Shows the part of the form for Empty, Template or Import. */
export function applyStart(form, start) {
  for (const button of form.querySelectorAll('[data-new-start] [data-start]')) {
    button.setAttribute('aria-pressed', String(button.getAttribute('data-start') === start));
  }
  for (const panel of form.querySelectorAll('[data-start-panel]')) {
    panel.hidden = panel.getAttribute('data-start-panel') !== start;
  }
  for (const el of form.querySelectorAll('[data-start-not-import]')) el.hidden = start === 'import';
  if (start === 'empty') clearTemplate(form);
}

/** "Start from": one form for the three ways in; the import links follow what is typed. */
export function initNewRepoStart(form = document.querySelector('.rp-new-form')) {
  if (!form?.querySelector('[data-new-start]')) return;
  form.querySelector('[data-new-start]').addEventListener('click', (e) => {
    const button = e.target.closest('[data-start]');
    if (button) applyStart(form, button.getAttribute('data-start'));
  });
  const links = () => {
    for (const a of form.querySelectorAll('[data-import-link]')) a.setAttribute('href', importHref(a.getAttribute('href'), form));
  };
  form.addEventListener('input', links);
  form.addEventListener('change', links);
  links();
}

let timer = 0;

/** Checks the name of a new repository 300 ms after the last key, for the owner chosen. */
export function initRepoNew() {
  initOwnerVisibility();
  initNewRepoPreview();
  initNewRepoStart();
  const input = document.getElementById('repo_name');
  const line = document.getElementById('repo_name_status');
  if (!input || !line) return;
  const run = async () => {
    const name = input.value.trim();
    if (!name) {
      applyNameCheck(line, null);
      return;
    }
    const q = new URLSearchParams({name, uid: document.getElementById('uid')?.value || ''});
    try {
      const res = await GET(`${line.getAttribute('data-check-url')}?${q}`, {headers: {Accept: 'application/json'}});
      if (!res.ok) return;
      const answer = await res.json();
      if (input.value.trim() === name) applyNameCheck(line, answer);
    } catch { /* the name check is a hint; the form still decides */ }
  };
  const later = () => {
    clearTimeout(timer);
    timer = setTimeout(run, 300);
  };
  input.addEventListener('input', later);
  document.getElementById('uid')?.addEventListener('change', later);
}
