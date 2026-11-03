import {pickTeam} from './org-team.js';

const card = (name, selected) => `<li class="ds-card wk-team-card${selected ? ' is-selected' : ''}">
  <a class="wk-team-pick-link" href="/org/acme/teams?team=${name}#team-detail"${selected ? ' aria-current="true"' : ''}>${name}</a></li>`;
const detail = (name) => `<section class="ds-card wk-team-detail" id="team-detail"><h2>${name}</h2>
  <button type="button" class="delete-button" data-modal-id="leave-team" data-name="${name}">Leave</button></section>`;

function refuse() {
  throw new Error('asked');
}

function page() {
  document.body.innerHTML = `<div class="wk-org-body"><ul>${card('owners', true)}${card('core', false)}</ul>${detail('owners')}</div>`;
  return document.querySelectorAll('.wk-team-pick-link');
}

test('choosing a team puts its detail under the cards and marks its card', async () => {
  const [owners, core] = page();
  const asked = [];
  const load = (url) => {
    asked.push(url);
    return `<html><body><div class="wk-org-body">${detail('core')}</div></body></html>`;
  };
  expect(await pickTeam(core, load)).toBe(true);
  expect(asked).toEqual([core.href]);
  expect(document.querySelectorAll('#team-detail')).toHaveLength(1);
  expect(document.querySelector('#team-detail h2').textContent).toBe('core');
  expect(document.activeElement).toBe(document.querySelector('#team-detail h2'));
  expect(core.getAttribute('aria-current')).toBe('true');
  expect(core.closest('.wk-team-card').classList.contains('is-selected')).toBe(true);
  expect(owners.hasAttribute('aria-current')).toBe(false);
  expect(owners.closest('.wk-team-card').classList.contains('is-selected')).toBe(false);
});

test('an answer without a detail leaves the page as it was, so the link is followed', async () => {
  const [owners, core] = page();
  expect(await pickTeam(core, () => '<html><body><p>Sign in</p></body></html>')).toBe(false);
  expect(document.querySelector('#team-detail h2').textContent).toBe('owners');
  expect(owners.getAttribute('aria-current')).toBe('true');
});

test('a page without a detail panel asks for nothing', async () => {
  document.body.innerHTML = `<div class="wk-org-body"><ul>${card('core', false)}</ul></div>`;
  expect(await pickTeam(document.querySelector('.wk-team-pick-link'), refuse)).toBe(false);
});
