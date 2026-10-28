import {initLivePreview, previewText} from './ds-forms.js';

test('previewText uses the fallback when the field is empty', () => {
  document.body.innerHTML = '<form id="f"><input name="full_name" value="  "><input name="name" value="maya"></form>';
  const form = document.getElementById('f');
  expect(previewText(form, 'full_name', 'name')).toBe('maya');
  form.elements.full_name.value = 'Maya Okafor';
  expect(previewText(form, 'full_name', 'name')).toBe('Maya Okafor');
  expect(previewText(form, 'missing')).toBe('');
});

test('the live preview follows the form and hides empty rows', () => {
  document.body.innerHTML = `<form id="f"><input name="location" value=""><input name="name" value="maya"></form>
    <div data-preview-form="f"><b data-preview-field="name"></b><i data-preview-field="location" data-preview-empty></i></div>`;
  initLivePreview();
  const loc = document.querySelector('i');
  expect(document.querySelector('b').textContent).toBe('maya');
  expect(loc.hidden).toBe(true);
  const input = document.querySelector('input[name="location"]');
  input.value = 'London';
  input.dispatchEvent(new Event('input', {bubbles: true}));
  expect(loc.textContent).toBe('London');
  expect(loc.hidden).toBe(false);
});

test('the layout toggle shows, applies the saved layout and keeps a new choice', async () => {
  const {initLayoutToggle} = await import('./ds-forms.js');
  localStorage.setItem('asc-layout', 'grid');
  document.body.innerHTML = `<div data-layout-toggle data-layout-target=".l" hidden>
    <button data-layout-set="list" aria-pressed="true"></button><button data-layout-set="grid" aria-pressed="false"></button></div><ul class="l"></ul>`;
  initLayoutToggle();
  expect(document.querySelector('[data-layout-toggle]').hidden).toBe(false);
  expect(document.querySelector('.l').classList.contains('is-grid')).toBe(true);
  expect(document.querySelector('[data-layout-set="grid"]').getAttribute('aria-pressed')).toBe('true');
  document.querySelector('[data-layout-set="list"]').click();
  expect(document.querySelector('.l').classList.contains('is-grid')).toBe(false);
  expect(localStorage.getItem('asc-layout')).toBe('list');
});
