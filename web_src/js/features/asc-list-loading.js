import {reloadsList} from './issue-list-loading.js';

/** Shows the skeleton card of a list in place of the list, the empty state and the pager. */
export function showListLoading(main) {
  const card = main.querySelector('[data-asc-list-loading]');
  if (!card || main.classList.contains('is-loading')) return;
  main.classList.add('is-loading');
  card.hidden = false;
}

/** Gives the list back, for a page restored from the back-forward cache. */
export function hideListLoading(main) {
  main.classList.remove('is-loading');
  const card = main.querySelector('[data-asc-list-loading]');
  if (card) card.hidden = true;
}

/**
 * The runs of Agents, the definitions of Workflows and the policy gate (templates/repo/asc_agents.tmpl,
 * asc_workflows.tmpl, asc_gate.tmpl): a state chip, the agent filter, the search, a page of the pager or the window
 * switch loads the page again, and until it does the list shows skeleton rows.
 */
export function initAscListLoading(doc = document) {
  const main = doc.querySelector('[data-asc-list-main]');
  if (!main) return;
  const filters = doc.querySelector('.rv-agents') ?? doc;
  filters.addEventListener('click', (e) => {
    const link = e.target.closest?.('a[href]');
    if (link && reloadsList(link, e)) showListLoading(main);
  });
  filters.addEventListener('submit', (e) => {
    if (!e.defaultPrevented && e.target.matches('form.ag-search')) showListLoading(main);
  });
  window.addEventListener('pageshow', (e) => {
    if (e.persisted) hideListLoading(main);
  });
}
