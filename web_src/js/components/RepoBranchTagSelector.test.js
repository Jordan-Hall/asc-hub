import {flushPromises, mount} from '@vue/test-utils';
import RepoBranchTagSelector from './RepoBranchTagSelector.vue';
import {GET} from '../modules/fetch.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn()}));

function respond(ok, body) {
  return {ok, json: () => Promise.resolve(body)};
}

function mountSelector(extra = {}) {
  return mount({
    ...RepoBranchTagSelector,
    data() {
      return {
        items: [], searchTerm: '', refNameText: '', menuVisible: false, release: null,
        isViewTag: false, isViewBranch: false, isViewTree: false, active: 0, isLoading: false,
        hasListInitialized: {branches: false, tags: false, asc: false}, ascBranchless: false, fallbackMode: '',
        mode: 'branches', showBranchesInDropdown: true, searchFieldPlaceholder: 'Filter branch or tag...',
        viewType: 'branch', branchName: 'main', defaultSelectedRefName: 'main', repoDefaultBranch: 'main',
        repoLink: '/acme/payments', branchURLPrefix: '/acme/payments/src/branch/', branchURLSuffix: '',
        tagURLPrefix: '/acme/payments/src/tag/', tagURLSuffix: '', disableCreateBranch: false,
        noResults: 'No results', textBranches: 'Branches', textTags: 'Tags',
        ascRefs: true, textAscLabel: 'Switch version', textAscFind: 'Find a ref, stack or tag',
        textAscKinds: {shared: 'Shared ref', stack: 'Stack', tag: 'Tag'},
        textAscBranchless: 'Branchless repository: stacks and tags are listed next to the shared refs.',
        ...extra,
      };
    },
  });
}

const menu = {
  branchless: true,
  refs: [
    {name: 'main', kind: 'shared'},
    {name: 'retry-with-backoff', kind: 'stack'},
    {name: 'v2.18.0', kind: 'tag'},
  ],
};

beforeEach(() => {
  GET.mockReset();
});

test('an asc repository lists shared refs, stacks and tags as one list', async () => {
  GET.mockResolvedValue(respond(true, menu));
  const w = mountSelector();
  await w.find('button').trigger('click');
  await flushPromises();
  expect(GET).toHaveBeenCalledWith('/acme/payments/asc/refs');
  const q = (sel) => w.findAll(sel).map((e) => e.text());
  expect(w.find('.menu').attributes('role')).toBe('dialog');
  expect(w.find('.menu').attributes('aria-label')).toBe('Switch version');
  expect(w.find('input').attributes('placeholder')).toBe('Find a ref, stack or tag');
  expect(w.find('.branch-tag-tab').exists()).toBe(false);
  expect(q('.asc-ref-name')).toEqual(['main', 'retry-with-backoff', 'v2.18.0']);
  expect(q('.asc-ref-kind')).toEqual(['Shared ref', 'Stack', 'Tag']);
  expect(q('.asc-ref-check')).toEqual(['✓', '', '']);
  expect(w.find('.asc-refs-note').text()).toContain('Branchless repository');

  await w.find('input').setValue('retry');
  expect(q('.asc-ref-name')).toEqual(['retry-with-backoff']);
  expect(w.text()).not.toContain('Create branch');
});

test('the note is shown only for a branchless repository', async () => {
  GET.mockResolvedValue(respond(true, {...menu, branchless: false}));
  const w = mountSelector();
  await w.find('button').trigger('click');
  await flushPromises();
  expect(w.find('.asc-refs-note').exists()).toBe(false);
});

test('a tag links with the tag prefix, a stack with the branch prefix', async () => {
  GET.mockResolvedValue(respond(true, menu));
  const loc = {href: ''};
  vi.stubGlobal('location', loc);
  const w = mountSelector();
  await w.find('button').trigger('click');
  await flushPromises();
  const rows = w.findAll('.scrolling.menu .item');
  await rows[2].trigger('click');
  expect(loc.href).toBe('/acme/payments/src/tag/v2.18.0');
  await rows[1].trigger('click');
  expect(loc.href).toBe('/acme/payments/src/branch/retry-with-backoff');
  vi.unstubAllGlobals();
});

test('without the asc menu the usual branches list is shown', async () => {
  GET.mockResolvedValueOnce(respond(false, {}));
  GET.mockResolvedValueOnce(respond(true, {results: ['main', 'dev']}));
  const w = mountSelector();
  await w.find('button').trigger('click');
  await flushPromises();
  expect(GET).toHaveBeenLastCalledWith('/acme/payments/branches/list');
  expect(w.find('.branch-tag-tab').exists()).toBe(true);
  expect(w.findAll('.scrolling.menu .item').map((e) => e.text().trim())).toEqual(['main', 'dev']);
});
