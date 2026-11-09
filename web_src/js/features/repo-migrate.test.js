import {initRepoMigrationStatusChecker} from './repo-migrate.js';
import {GET} from '../modules/fetch.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn(), POST: vi.fn()}));

const page = `<div id="repo_migrating" data-migrating-task-id="7"></div>
<div id="repo_migrating_progress"><progress class="rp-migrating-bar" aria-label="The import is running"></progress><p id="repo_migrating_progress_message"></p></div>`;

function answer(data) {
  GET.mockResolvedValueOnce({url: '/user/task/7', status: 200, json: () => Promise.resolve(data)});
}

describe('the migration status', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    document.body.innerHTML = page;
  });
  afterEach(() => {
    vi.useRealTimers();
    GET.mockReset();
  });

  test('the bar shows how far the history import is, and only while the migration says', async () => {
    const bar = document.querySelector('.rp-migrating-bar');
    answer({status: 2, message: 'Importing history: 3,412 of 9,880 history entries', done: 3412, total: 9880});
    initRepoMigrationStatusChecker();
    await vi.waitFor(() => expect(bar.value).toBe(3412));
    expect(bar.max).toBe(9880);
    expect(document.querySelector('#repo_migrating_progress_message').textContent).toBe('Importing history: 3,412 of 9,880 history entries');
    expect(GET).toHaveBeenCalledWith(expect.stringContaining('/user/task/7'));

    // The next step has no counts: the bar goes back to showing that something runs.
    answer({status: 2, message: 'Migrating issues', done: 0, total: 0});
    await vi.advanceTimersByTimeAsync(2000);
    await vi.waitFor(() => expect(bar.hasAttribute('value')).toBe(false));
    expect(document.querySelector('#repo_migrating_progress_message').textContent).toBe('Migrating issues');
  });
});
