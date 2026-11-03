import {hideListLoading, reloadsList, showListLoading} from './issue-list-loading.js';

const here = new URL('https://hub.example.org/acme/nova/issues?state=open');
const click = (extra = {}) => ({defaultPrevented: false, button: 0, metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, ...extra});
const link = (href, attrs = {}) => {
  const a = document.createElement('a');
  a.href = new URL(href, here).href;
  for (const [k, v] of Object.entries(attrs)) a.setAttribute(k, v);
  return a;
};

test('a filter of the same list reloads it; other pages, new tabs and modified clicks do not', () => {
  expect(reloadsList(link('?state=closed'), click(), here)).toBe(true);
  expect(reloadsList(link('/acme/nova/issues?labels=3'), click(), here)).toBe(true);
  expect(reloadsList(link('/acme/nova/issues'), click(), here)).toBe(true);
  expect(reloadsList(link('?state=open'), click(), here)).toBe(false);
  expect(reloadsList(link('/acme/nova/issues/4'), click(), here)).toBe(false);
  expect(reloadsList(link('https://other.example.org/acme/nova/issues?state=closed'), click(), here)).toBe(false);
  expect(reloadsList(link('?state=closed', {target: '_blank'}), click(), here)).toBe(false);
  expect(reloadsList(link('?state=closed'), click({ctrlKey: true}), here)).toBe(false);
  expect(reloadsList(link('?state=closed'), click({button: 1}), here)).toBe(false);
  expect(reloadsList(link('?state=closed'), click({defaultPrevented: true}), here)).toBe(false);
});

test('loading hides the rows behind skeletons once, and a restored page gets them back', () => {
  document.body.innerHTML = '<ul id="issue-list" data-loading="Loading issues"><li class="ds-li">one</li></ul>';
  const list = document.querySelector('#issue-list');
  showListLoading(list);
  showListLoading(list);
  expect(list.getAttribute('aria-busy')).toBe('true');
  expect(list.querySelectorAll('.wk-issue-sk[aria-hidden="true"]')).toHaveLength(5);
  expect(list.querySelector('[role="status"]').textContent).toBe('Loading issues');
  hideListLoading(list);
  expect(list.hasAttribute('aria-busy')).toBe(false);
  expect(list.querySelectorAll('.wk-issue-sk')).toHaveLength(0);
  expect(list.querySelectorAll('li')).toHaveLength(1);
});
