import $ from 'jquery';
import {GET} from '../modules/fetch.js';
import {initDeleteButtons} from './delete-button.js';

const {appSubUrl} = window.config;

export function initOrgTeamSearchRepoBox() {
  const $searchRepoBox = $('#search-repo-box');
  $searchRepoBox.search({
    minCharacters: 2,
    apiSettings: {
      url: `${appSubUrl}/repo/search?q={query}&uid=${$searchRepoBox.data('uid')}`,
      onResponse(response) {
        const items = [];
        for (const item of response.data) {
          items.push({
            title: item.repository.full_name.split('/')[1],
            description: item.repository.full_name,
          });
        }
        return {results: items};
      },
    },
    searchFields: ['full_name'],
    showNoResults: false,
  });
}

async function loadPage(url) {
  const res = await GET(url);
  if (!res.ok) throw new Error(`${url}: ${res.status}`);
  return res.text();
}

/**
 * The team cards of the Teams page (templates/org/team/teams.tmpl): choosing one puts its detail (people and
 * repositories) under the cards in place of the one shown. Returns false when the page or the answer has no detail,
 * so that the caller follows the link instead.
 */
export async function pickTeam(link, load = loadPage) {
  const body = link.closest('.wk-org-body');
  const detail = body?.querySelector('#team-detail');
  if (!detail) return false;
  const next = new DOMParser().parseFromString(await load(link.href), 'text/html').querySelector('#team-detail');
  if (!next) return false;
  detail.replaceWith(next);
  for (const pick of body.querySelectorAll('.wk-team-pick-link')) {
    const on = pick.href === link.href;
    pick.closest('.wk-team-card')?.classList.toggle('is-selected', on);
    if (on) pick.setAttribute('aria-current', 'true');
    else pick.removeAttribute('aria-current');
  }
  initDeleteButtons(next);
  const heading = next.querySelector('h2');
  heading?.setAttribute('tabindex', '-1');
  heading?.focus();
  return true;
}

async function followTeam(link, push) {
  try {
    if (await pickTeam(link)) {
      if (push) window.history.pushState({ascTeam: true}, '', link.href);
      return;
    }
  } catch {}
  window.location.assign(link.href);
}

export function initOrgTeamPicker() {
  if (!document.querySelector('.wk-team-pick-link')) return;
  document.addEventListener('click', (e) => {
    const link = e.target.closest?.('.wk-team-pick-link');
    if (!link || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    followTeam(link, true);
  });
  window.addEventListener('popstate', () => {
    const link = Array.from(document.querySelectorAll('.wk-team-pick-link')).find((a) => a.href === window.location.href);
    if (link) followTeam(link, false);
    else window.location.reload();
  });
}
