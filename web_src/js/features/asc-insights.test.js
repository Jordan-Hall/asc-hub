import {GET} from '../modules/fetch.js';
import {initInsightsBoard, loadInsights} from './asc-insights.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn()}));

function boardHtml(range, landed) {
  return `<div data-ins-board data-testid="asc-insights">
    <div data-ins-segs><a href="/o/r/activity?range=7d"${range === '7d' ? ' aria-current="true"' : ''}>7 days</a><a href="/o/r/activity?range=30d"${range === '30d' ? ' aria-current="true"' : ''}>30 days</a></div>
    <p data-ins-status data-loading="Loading insights" role="status"></p>
    <template data-ins-skeleton><div data-testid="asc-insights-loading"></div></template>
    <div data-ins-body><dd data-landed>${landed}</dd></div>
  </div>`;
}

function mount() {
  document.body.innerHTML = boardHtml('30d', 4);
  const board = document.querySelector('[data-ins-board]');
  initInsightsBoard(board);
  return board;
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

afterEach(() => {
  vi.restoreAllMocks();
  GET.mockReset();
});

test('a period loads in place behind a skeleton and the board is swapped whole', async () => {
  let answer;
  GET.mockReturnValue(new Promise((resolve) => {
    answer = resolve;
  }));
  const push = vi.spyOn(window.history, 'pushState');
  const board = mount();
  document.querySelector('a[href$="7d"]').click();
  expect(GET).toHaveBeenCalledWith('/o/r/activity?range=7d', expect.objectContaining({signal: expect.any(AbortSignal)}));
  expect(board.getAttribute('aria-busy')).toBe('true');
  expect(board.querySelector('[data-testid="asc-insights-loading"]')).not.toBeNull();
  expect(board.querySelector('[data-landed]')).toBeNull();
  expect(board.querySelector('[data-ins-status]').textContent).toBe('Loading insights');

  answer({ok: true, text: () => Promise.resolve(`<html><body><main>${boardHtml('7d', 9)}</main></body></html>`)});
  await flush();
  await flush();
  const next = document.querySelector('[data-ins-board]');
  expect(next).not.toBe(board);
  expect(next.hasAttribute('aria-busy')).toBe(false);
  expect(next.querySelector('[data-landed]').textContent).toBe('9');
  expect(next.querySelector('a[aria-current="true"]').getAttribute('href')).toBe('/o/r/activity?range=7d');
  expect(push).toHaveBeenCalledWith({ascInsights: true}, '', '/o/r/activity?range=7d');
});

test('a modified click opens the link as usual', () => {
  mount();
  const a = document.querySelector('a[href$="7d"]');
  a.dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true, ctrlKey: true}));
  expect(GET).not.toHaveBeenCalled();
});

test('a failed load opens the link so the server draws its error state', async () => {
  const assign = vi.fn();
  vi.spyOn(window, 'location', 'get').mockReturnValue({...window.location, assign});
  GET.mockResolvedValue({ok: false, status: 502});
  const board = mount();
  await loadInsights(board, '/o/r/activity?range=7d', true);
  expect(assign).toHaveBeenCalledWith('/o/r/activity?range=7d');

  GET.mockRejectedValue(new TypeError('offline'));
  await loadInsights(document.querySelector('[data-ins-board]'), '/o/r/activity?range=30d', true);
  expect(assign).toHaveBeenCalledWith('/o/r/activity?range=30d');
});
