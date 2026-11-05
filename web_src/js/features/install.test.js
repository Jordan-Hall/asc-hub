import {dbSignature, initInstall, setRun, summaryHost} from './install.js';
import {GET, POST} from '../modules/fetch.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn(), POST: vi.fn()}));

const check = (wrap, id) => `<div class="ds-check" id="${wrap}"><input id="${id}" type="checkbox"></div>`;

const page = `<div class="page-content install"><form>
<div data-ins-mstep data-label="First run · step {n} of {total}" hidden><p data-ins-mstep-label></p><progress max="4" value="1" data-ins-mstep-bar></progress></div>
<section id="ins-db" data-ins-step="1"><h2>Database</h2>
  <select id="db_type"><option value="sqlite3">SQLite3</option><option value="postgres" selected>PostgreSQL</option></select>
  <div class="ds-banner ds-banner-err" data-ins-db-error tabindex="-1" hidden><p><b data-ins-db-error-head>Connection failed.</b> <span data-ins-db-error-msg></span></p></div>
  <div data-db-setting-for="common-host" class="tw-hidden">
    <div class="ds-field"><input id="db_host" value="db.example.org:5432"></div>
    <div class="ds-field"><input id="db_name" value="hub"></div>
    <div class="ds-field"><input id="db_user" value="asc"></div>
    <div class="ds-field"><input id="db_passwd" type="password" value="pw"></div>
  </div>
  <div data-db-setting-for="postgres" class="tw-hidden"><select id="ssl_mode"><option value="disable">Disable</option></select><div class="ds-field"><input id="db_schema"></div></div>
  <div data-db-setting-for="sqlite3" class="tw-hidden"><div class="ds-field"><input id="db_path" value="/data/asc.db"></div></div>
  <div data-ins-db-test-row><button type="button" data-ins-db-test data-url="/test-db">Test connection</button><span data-ins-db-status data-testing="Testing" data-unreachable="No answer" data-fail-head="Connection failed."></span></div>
  <div data-ins-db-error-actions hidden><button type="button" data-ins-db-test data-url="/test-db">Test again</button><button type="button" data-ins-sqlite>Use SQLite instead</button></div>
  <template data-ins-icon-err><svg></svg></template><template data-ins-icon-ok><svg></svg></template>
</section>
<section id="ins-general" data-ins-step="2"><h2>General</h2>
  <input id="app_name" required value="ASC Hub"><input id="app_url" value="https://hub.example.org/"><input id="domain" value="hub.example.org">
  ${check('disable-registration', 'disable_registration')}
</section>
<section id="ins-admin" data-ins-step="3"><h2>Administrator</h2>
  <input id="admin_name"><input id="admin_email" type="email"><input id="admin_passwd"><input id="admin_confirm_passwd">
</section>
<section id="ins-review" data-ins-step="4"><h2>Optional</h2>
  ${check('offline-mode', 'offline_mode')}${check('disable-gravatar', 'disable_gravatar')}${check('federated-avatar-lookup', 'fed')}
  ${check('enable-openid-signin', 'oid_in')}${check('enable-openid-signup', 'oid_up')}${check('enable-captcha', 'captcha')}
</section>
<div data-ins-mnav hidden><button type="button" data-ins-back>Back</button><button type="button" data-ins-next data-next="Next" data-next-review="Next: review">Next</button></div>
<div data-ins-step="4"><button type="submit" data-ins-submit>Install ASC Hub</button></div>
<ol><li data-ins-step-for="1"><a href="#ins-db">1</a><span data-ins-step-done hidden>(done)</span></li><li data-ins-step-for="2"><a href="#ins-general">2</a></li><li data-ins-step-for="3"><a href="#ins-admin">3</a></li><li data-ins-step-for="4"><a href="#ins-review">4</a></li></ol>
<dl data-reg-open="Open" data-reg-invite="Invite only"><dd data-ins-sum-db></dd><dd data-ins-sum-url></dd><dd data-ins-sum-reg></dd></dl>
<p data-ins-submit-hint hidden>Test first</p>
</form></div>`;

const q = (sel) => document.querySelector(sel);

const runStep = (label) => `<li data-ins-run-step><span>${label}</span><span data-ins-run-state></span></li>`;
const run = (done) => `<div data-ins-run data-done="${done}" data-label="Step {n} of {total}" data-t-done="(done)" data-t-now="(in progress)" data-t-todo="(waiting)"><p data-ins-run-label></p><progress max="10" value="0" data-ins-run-bar></progress><ol>${['Tables', 'Root', 'Admin', 'Queue', 'Open'].map(runStep).join('')}</ol></div>`;
const states = () => Array.from(document.querySelectorAll('[data-ins-run-state]'), (el) => el.textContent);
const flush = () => new Promise((resolve) => setTimeout(resolve, 0));
const answer = (body) => POST.mockResolvedValueOnce({json: () => Promise.resolve(body)});

function phone(matches) {
  window.matchMedia = () => ({matches, addEventListener: vi.fn()});
}

beforeEach(() => {
  POST.mockReset();
  GET.mockReset();
  phone(false);
  document.body.innerHTML = page;
});

test('summaryHost shows the host of the public URL, or the domain', () => {
  expect(summaryHost('https://hub.example.org:8443/', 'x')).toBe('hub.example.org:8443');
  expect(summaryHost('not a url', 'hub.example.org')).toBe('hub.example.org');
});

test('a server database keeps Install locked until a test passes for the current values', async () => {
  initInstall();
  const submit = q('[data-ins-submit]');
  expect(submit.disabled).toBe(true);
  expect(q('[data-ins-submit-hint]').hidden).toBe(false);
  expect(q('[data-ins-sum-db]').textContent).toBe('PostgreSQL');
  expect(q('[data-ins-sum-url]').textContent).toBe('hub.example.org');
  expect(q('[data-ins-sum-reg]').textContent).toBe('Open');

  answer({ok: true, message: 'Connected to PostgreSQL 16.4', version: '16.4'});
  q('[data-ins-db-test]').click();
  await flush();
  const [url, opts] = POST.mock.calls[0];
  expect(url).toBe('/test-db');
  expect(opts.headers).toEqual({'X-Asc-Install': '1'});
  expect(opts.data.get('db_host')).toBe('db.example.org:5432');
  expect(submit.disabled).toBe(false);
  expect(q('[data-ins-db-status]').textContent).toBe('Connected to PostgreSQL 16.4');
  expect(q('[data-ins-sum-db]').textContent).toBe('PostgreSQL 16');
  expect(q('[data-ins-step-for="1"]').classList.contains('is-done')).toBe(true);
  expect(q('[data-ins-step-done]').hidden).toBe(false);

  q('#db_passwd').value = 'other';
  q('#db_passwd').dispatchEvent(new Event('input', {bubbles: true}));
  expect(submit.disabled).toBe(true);
  expect(q('[data-ins-db-status]').textContent).toBe('');
});

test('a failed test shows the error card, marks the field it names and focuses it', async () => {
  initInstall();
  answer({ok: false, head: 'Connection failed.', message: 'ASC Hub could not connect to db.example.org:5432: password authentication failed for user asc. Nothing was changed.', field: 'db_passwd', hint: 'Check the password for this database user.'});
  q('[data-ins-db-test]').click();
  await flush();
  expect(q('[data-ins-db-error]').hidden).toBe(false);
  expect(q('[data-ins-db-error-msg]').textContent).toContain('password authentication failed');
  expect(q('[data-ins-db-error-actions]').hidden).toBe(false);
  expect(q('[data-ins-db-test-row]').hidden).toBe(true);
  const pw = q('#db_passwd');
  expect(pw.getAttribute('aria-invalid')).toBe('true');
  expect(pw.getAttribute('aria-describedby')).toBe('db_passwd-dberr');
  expect(q('#db_passwd-dberr').textContent).toBe('Check the password for this database user.');
  expect(document.activeElement).toBe(pw);

  // Test again clears the marks when it passes
  answer({ok: true, message: 'Connected to PostgreSQL 16.4', version: '16.4'});
  q('[data-ins-db-error-actions] [data-ins-db-test]').click();
  await flush();
  expect(q('[data-ins-db-error]').hidden).toBe(true);
  expect(pw.hasAttribute('aria-invalid')).toBe(false);
  expect(q('#db_passwd-dberr')).toBeNull();
});

test('a network failure says the installer did not answer', async () => {
  initInstall();
  POST.mockRejectedValueOnce(new Error('offline'));
  q('[data-ins-db-test]').click();
  await flush();
  expect(q('[data-ins-db-error-msg]').textContent).toBe('No answer');
  expect(document.activeElement).toBe(q('[data-ins-db-error]'));
});

test('Use SQLite instead switches the database, unlocks Install and focuses the path', async () => {
  initInstall();
  answer({ok: false, message: 'refused', field: 'db_host', hint: 'Check the host'});
  q('[data-ins-db-test]').click();
  await flush();
  q('[data-ins-sqlite]').click();
  expect(q('#db_type').value).toBe('sqlite3');
  expect(q('[data-ins-db-error]').hidden).toBe(true);
  expect(q('[data-ins-db-test-row]').hidden).toBe(false);
  expect(q('[data-ins-submit]').disabled).toBe(false);
  expect(q('[data-ins-sum-db]').textContent).toBe('SQLite3');
  expect(document.activeElement).toBe(q('#db_path'));
  expect(q('[data-ins-sqlite]').hidden).toBe(true);
});

test('the step list marks the first unfinished step and the summary follows registration', () => {
  q('#db_type').value = 'sqlite3';
  initInstall();
  expect(q('[data-ins-step-for="1"]').classList.contains('is-done')).toBe(true);
  expect(q('[data-ins-step-for="2"]').classList.contains('is-done')).toBe(true);
  // no administrator while registration is open is fine: the first user owns the hub
  expect(q('[data-ins-step-for="3"]').classList.contains('is-done')).toBe(true);
  expect(q('[data-ins-step-for="4"] a').getAttribute('aria-current')).toBe('step');

  const reg = q('#disable_registration');
  reg.checked = true;
  reg.dispatchEvent(new Event('change', {bubbles: true}));
  expect(q('[data-ins-sum-reg]').textContent).toBe('Invite only');
  expect(q('[data-ins-step-for="3"] a').getAttribute('aria-current')).toBe('step');

  for (const [id, v] of [['admin_name', 'root'], ['admin_email', 'root@example.org'], ['admin_passwd', 'pw-123456'], ['admin_confirm_passwd', 'pw-123456']]) q(`#${id}`).value = v;
  q('#admin_name').dispatchEvent(new Event('input', {bubbles: true}));
  expect(q('[data-ins-step-for="3"]').classList.contains('is-done')).toBe(true);
});

test('on a phone one section shows at a time with Back and Next', () => {
  phone(true);
  initInstall();
  const form = q('form');
  expect(form.classList.contains('ins-phone')).toBe(true);
  expect(q('[data-ins-mstep]').hidden).toBe(false);
  expect(q('[data-ins-mstep-label]').textContent).toBe('First run · step 1 of 4');
  expect(q('#ins-db').classList.contains('ins-off')).toBe(false);
  expect(q('#ins-general').classList.contains('ins-off')).toBe(true);
  expect(q('[data-ins-back]').hidden).toBe(true);

  q('[data-ins-next]').click();
  q('[data-ins-next]').click();
  expect(q('[data-ins-mstep-label]').textContent).toBe('First run · step 3 of 4');
  expect(q('[data-ins-mstep-bar]').value).toBe(3);
  expect(q('[data-ins-next]').textContent).toBe('Next: review');
  expect(document.activeElement).toBe(q('#ins-admin h2'));

  q('[data-ins-next]').click();
  expect(q('[data-ins-next]').hidden).toBe(true);
  expect(q('[data-ins-submit]').closest('[data-ins-step]').classList.contains('ins-off')).toBe(false);

  q('[data-ins-back]').click();
  expect(q('#ins-admin').classList.contains('ins-off')).toBe(false);
});

test('Next stays on a section with an invalid field', () => {
  phone(true);
  initInstall();
  q('[data-ins-next]').click();
  q('#app_name').value = '';
  q('#app_name').reportValidity = vi.fn();
  q('[data-ins-next]').click();
  expect(q('#app_name').reportValidity).toHaveBeenCalled();
  expect(q('[data-ins-mstep-label]').textContent).toBe('First run · step 2 of 4');
});

test('a phone opens on the section with a server error', () => {
  phone(true);
  q('[data-ins-db-error]').hidden = false;
  initInstall();
  expect(q('[data-ins-mstep-label]').textContent).toBe('First run · step 1 of 4');
  document.body.innerHTML = page;
  q('#admin_email').setAttribute('aria-invalid', 'true');
  initInstall();
  expect(q('[data-ins-mstep-label]').textContent).toBe('First run · step 3 of 4');
});

test('dbSignature changes with any database field', () => {
  const form = new DOMParser().parseFromString(page, 'text/html').querySelector('form');
  const before = dbSignature(form);
  form.querySelector('#db_schema').value = 'asc';
  expect(dbSignature(form)).not.toBe(before);
});

test('the post-install page waits for the hub, then shows the ready state', async () => {
  vi.useFakeTimers();
  document.body.innerHTML = `<div class="page-content install post-install"><section data-ins-poll="/user/login"><div data-ins-wait hidden><h1 tabindex="-1">Setting up</h1></div><div data-ins-ready><h1 tabindex="-1">ASC Hub is ready</h1></div></section></div>`;
  GET.mockResolvedValueOnce({status: 404}).mockResolvedValueOnce({status: 200});
  initInstall();
  expect(q('[data-ins-wait]').hidden).toBe(false);
  expect(q('[data-ins-ready]').hidden).toBe(true);
  await vi.advanceTimersByTimeAsync(1000);
  expect(q('[data-ins-ready]').hidden).toBe(true);
  await vi.advanceTimersByTimeAsync(1000);
  expect(q('[data-ins-ready]').hidden).toBe(false);
  expect(q('[data-ins-wait]').hidden).toBe(true);
  expect(document.activeElement.textContent).toBe('ASC Hub is ready');
  expect(GET).toHaveBeenCalledWith('/user/login');
  vi.useRealTimers();
});

test('setRun marks the steps done, the next one running and the rest waiting', () => {
  document.body.innerHTML = run(0);
  const el = q('[data-ins-run]');
  setRun(el, 2);
  expect(q('[data-ins-run-label]').textContent).toBe('Step 3 of 5');
  expect(states()).toEqual(['(done)', '(done)', '(in progress)', '(waiting)', '(waiting)']);
  expect(Array.from(el.querySelectorAll('li'), (li) => li.className)).toEqual(['is-done', 'is-done', 'is-cur', '', '']);
  expect(q('[data-ins-run-bar]').value).toBe(5);
  setRun(el, 5);
  expect(q('[data-ins-run-label]').textContent).toBe('Step 5 of 5');
  expect(states().every((s) => s === '(done)')).toBe(true);
  expect(q('[data-ins-run-bar]').value).toBe(10);
});

test('sending the form swaps it for the progress, which follows the status route', async () => {
  vi.useFakeTimers();
  const form = q('form');
  q('#ins-db').classList.add('ins-split');
  form.insertAdjacentHTML('afterbegin', `<section data-ins-running data-url="/-/install/status" hidden><h2 tabindex="-1">Setting up your hub</h2>${run(0)}</section>`);
  GET.mockResolvedValue({ok: true, json: () => Promise.resolve({done: 2, total: 5})});
  initInstall();
  form.dispatchEvent(new Event('submit', {cancelable: true}));
  expect(q('[data-ins-running]').hidden).toBe(false);
  expect(q('#ins-db').classList.contains('tw-hidden')).toBe(true);
  expect(document.activeElement.textContent).toBe('Setting up your hub');
  expect(states()[0]).toBe('(in progress)');
  await vi.advanceTimersByTimeAsync(500);
  expect(GET).toHaveBeenCalledWith('/-/install/status');
  expect(q('[data-ins-run-label]').textContent).toBe('Step 3 of 5');
  vi.useRealTimers();
});

test('a form the page stopped is not swapped for the progress', () => {
  const form = q('form');
  form.insertAdjacentHTML('afterbegin', `<section data-ins-running data-url="/-/install/status" hidden>${run(0)}</section>`);
  initInstall();
  const stopped = new Event('submit', {cancelable: true});
  stopped.preventDefault();
  form.dispatchEvent(stopped);
  expect(q('[data-ins-running]').hidden).toBe(true);
});

test('the post-install page runs the merge queue step, then ticks every step', async () => {
  vi.useFakeTimers();
  document.body.innerHTML = `<div class="page-content install post-install"><section data-ins-poll="/user/login"><div data-ins-wait hidden><h1 tabindex="-1">Setting up</h1>${run(3)}</div><div data-ins-ready><h1 tabindex="-1">ASC Hub is ready</h1></div></section></div>`;
  GET.mockResolvedValueOnce({status: 200});
  initInstall();
  expect(states()).toEqual(['(done)', '(done)', '(done)', '(in progress)', '(waiting)']);
  expect(q('[data-ins-run-label]').textContent).toBe('Step 4 of 5');
  await vi.advanceTimersByTimeAsync(1000);
  expect(states().every((s) => s === '(done)')).toBe(true);
  expect(q('[data-ins-ready]').hidden).toBe(false);
  vi.useRealTimers();
});

test('the administrator is optional, all or nothing, and needs open registration when skipped', () => {
  q('#ins-admin').setAttribute('data-ins-admin', '');
  q('#ins-admin').setAttribute('data-skip-err', 'Keep registration open or add an administrator');
  initInstall();
  const name = q('#admin_name');
  expect(name.required).toBe(false);
  expect(name.validationMessage).toBe('');

  const reg = q('#disable_registration');
  reg.checked = true;
  reg.dispatchEvent(new Event('change', {bubbles: true}));
  expect(name.validationMessage).toBe('Keep registration open or add an administrator');

  q('#admin_email').value = 'root@example.org';
  q('#admin_email').dispatchEvent(new Event('input', {bubbles: true}));
  expect(name.validity.customError).toBe(false);
  expect(name.validity.valueMissing).toBe(true);
  for (const id of ['admin_name', 'admin_email', 'admin_passwd', 'admin_confirm_passwd']) expect(q(`#${id}`).required).toBe(true);
  expect(name.checkValidity()).toBe(false);

  q('#admin_email').value = '';
  reg.checked = false;
  reg.dispatchEvent(new Event('change', {bubbles: true}));
  expect(name.required).toBe(false);
  expect(name.checkValidity()).toBe(true);
});
