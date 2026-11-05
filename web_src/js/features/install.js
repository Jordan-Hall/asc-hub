import {hideElem, showElem} from '../utils/dom.js';
import {GET, POST} from '../modules/fetch.js';

// the fields a database test sends; a change to any of them asks for a new test
const dbFields = ['db_type', 'db_host', 'db_name', 'db_user', 'db_passwd', 'db_schema', 'ssl_mode', 'db_path'];
const stepCount = 4;
const phoneQuery = '(max-width: 767.98px)';

export function initInstall() {
  const page = document.querySelector('.page-content.install');
  if (!page) {
    return;
  }
  if (page.classList.contains('post-install')) {
    initPostInstall(page);
  } else {
    initPreInstall(page);
  }
}

function fieldValue(form, name) {
  const el = form.querySelector(`#${name}`);
  return el ? el.value : '';
}

// dbSignature is what was tested: Install unlocks only while the fields still match it.
export function dbSignature(form) {
  return dbFields.map((name) => fieldValue(form, name)).join('\n');
}

// summaryHost turns the public URL into the host the summary shows.
export function summaryHost(appUrl, domain) {
  try {
    return new URL(appUrl).host || domain;
  } catch {
    return domain;
  }
}

function initPreInstall(page) {
  const form = page.querySelector('form');
  const defaultDbUser = 'forgejo';
  const defaultDbName = 'forgejo';

  const defaultDbHosts = {
    mysql: '127.0.0.1:3306',
    postgres: '127.0.0.1:5432',
  };

  const dbType = document.getElementById('db_type');
  const dbHost = document.getElementById('db_host');
  const dbUser = document.getElementById('db_user');
  const dbName = document.getElementById('db_name');

  dbType.addEventListener('change', () => {
    const type = dbType.value;
    hideElem('div[data-db-setting-for]');
    showElem(`div[data-db-setting-for=${type}]`);

    if (type !== 'sqlite3') {
      // for most remote database servers
      showElem('div[data-db-setting-for=common-host]');
      const lastDbHost = dbHost.value;
      const isDbHostDefault = !lastDbHost || Object.values(defaultDbHosts).includes(lastDbHost);
      if (isDbHostDefault) {
        dbHost.value = defaultDbHosts[type] ?? '';
      }
      if (!dbUser.value && !dbName.value) {
        dbUser.value = defaultDbUser;
        dbName.value = defaultDbName;
      }
    } // else: for SQLite3, the default path is always prepared by backend code (setting)
  });
  dbType.dispatchEvent(new Event('change'));

  const appUrl = document.getElementById('app_url');
  if (appUrl.value.includes('://localhost')) {
    appUrl.value = window.location.href;
  }

  const domain = document.getElementById('domain');
  if (domain.value.trim() === 'localhost') {
    domain.value = window.location.hostname;
  }

  initExclusiveOptions();
  const progress = initProgress(form);
  initDbTest(form, progress);
  initPhoneStepper(form);
  initRunning(form);
  initAdmin(form);
  progress.update();
}

const adminFields = ['admin_name', 'admin_email', 'admin_passwd', 'admin_confirm_passwd'];

// initAdmin keeps the administrator optional: once one of its fields is filled all four are needed, and with no
// administrator self-registration must stay open, so the first person to register owns the hub.
function initAdmin(form) {
  const card = form.querySelector('[data-ins-admin]');
  const closed = document.getElementById('disable_registration');
  if (!card || !closed) return;
  const fields = adminFields.map((id) => document.getElementById(id)).filter(Boolean);
  const sync = () => {
    const any = fields.some((el) => el.value !== '');
    for (const el of fields) el.required = any;
    fields[0].setCustomValidity(closed.checked && !any ? card.getAttribute('data-skip-err') : '');
  };
  // on the form, so a box that another box clears (the exclusive options) is seen too
  form.addEventListener('input', sync);
  form.addEventListener('change', sync);
  sync();
}

function box(id) {
  return document.querySelector(`#${id} input`);
}

// initExclusiveOptions keeps the server options that rule each other out consistent.
function initExclusiveOptions() {
  box('offline-mode').addEventListener('change', (e) => {
    if (e.target.checked) {
      box('disable-gravatar').checked = true;
      box('federated-avatar-lookup').checked = false;
    }
  });
  box('disable-gravatar').addEventListener('change', (e) => {
    if (e.target.checked) {
      box('federated-avatar-lookup').checked = false;
    } else {
      box('offline-mode').checked = false;
    }
  });
  box('federated-avatar-lookup').addEventListener('change', (e) => {
    if (e.target.checked) {
      box('disable-gravatar').checked = false;
      box('offline-mode').checked = false;
    }
  });
  box('enable-openid-signin').addEventListener('change', (e) => {
    if (!e.target.checked) {
      box('enable-openid-signup').checked = false;
    } else if (!box('disable-registration').checked) {
      box('enable-openid-signup').checked = true;
    }
  });
  box('disable-registration').addEventListener('change', (e) => {
    if (e.target.checked) {
      box('enable-captcha').checked = false;
    }
    box('enable-openid-signup').checked = !e.target.checked;
  });
  box('enable-captcha').addEventListener('change', (e) => {
    if (e.target.checked) {
      box('disable-registration').checked = false;
    }
  });
}

// initProgress keeps the summary rail, the step list and the Install buttons in step with the form.
function initProgress(form) {
  const state = {okSignature: null, version: ''};

  const dbReady = () => {
    if (fieldValue(form, 'db_type') === 'sqlite3') {
      return fieldValue(form, 'db_path').trim() !== '';
    }
    return state.okSignature === dbSignature(form);
  };

  const sectionValid = (id) => {
    const section = form.querySelector(`#${id}`);
    return Array.from(section ? section.querySelectorAll('input, select') : []).every((el) => el.checkValidity());
  };

  const adminDone = () => {
    const values = ['admin_name', 'admin_email', 'admin_passwd', 'admin_confirm_passwd'].map((name) => fieldValue(form, name));
    if (values.every((v) => v === '')) {
      // skipping the administrator is allowed while people can still register
      return !form.querySelector('#disable_registration')?.checked;
    }
    return values.every((v) => v !== '') && values[2] === values[3] && sectionValid('ins-admin');
  };

  const update = () => {
    const ready = dbReady();
    for (const btn of form.querySelectorAll('[data-ins-submit]')) {
      btn.disabled = !ready;
    }
    for (const hint of form.querySelectorAll('[data-ins-submit-hint]')) {
      hint.hidden = ready;
    }

    const done = [ready, sectionValid('ins-general'), adminDone(), false];
    const current = done.indexOf(false) + 1;
    for (const li of form.querySelectorAll('[data-ins-step-for]')) {
      const n = Number(li.getAttribute('data-ins-step-for'));
      li.classList.toggle('is-done', done[n - 1]);
      li.classList.toggle('is-cur', n === current);
      const link = li.querySelector('a');
      if (n === current) {
        link.setAttribute('aria-current', 'step');
      } else {
        link.removeAttribute('aria-current');
      }
      const doneLabel = li.querySelector('[data-ins-step-done]');
      if (doneLabel) doneLabel.hidden = !done[n - 1];
    }

    const sumDb = form.querySelector('[data-ins-sum-db]');
    if (sumDb) {
      const type = form.querySelector('#db_type');
      const name = type.options[type.selectedIndex]?.textContent.trim() ?? '';
      const major = ready && state.version ? state.version.split('.')[0] : '';
      sumDb.textContent = major ? `${name} ${major}` : name;
    }
    const sumUrl = form.querySelector('[data-ins-sum-url]');
    if (sumUrl) sumUrl.textContent = summaryHost(fieldValue(form, 'app_url'), fieldValue(form, 'domain'));
    const sumReg = form.querySelector('[data-ins-sum-reg]');
    if (sumReg) {
      const list = sumReg.closest('[data-reg-open]');
      const invite = form.querySelector('#disable_registration')?.checked;
      sumReg.textContent = list.getAttribute(invite ? 'data-reg-invite' : 'data-reg-open');
    }
  };

  form.addEventListener('input', update);
  form.addEventListener('change', update);
  return {state, update};
}

function iconFrom(form, name) {
  const tpl = form.querySelector(`template[data-ins-icon-${name}]`);
  return tpl ? tpl.content.cloneNode(true) : document.createDocumentFragment();
}

// clearDbFieldErrors removes the marks a failed database test or install left on the fields.
function clearDbFieldErrors(form) {
  const card = form.querySelector('#ins-db');
  for (const msg of card.querySelectorAll('.ds-err-msg')) {
    msg.remove();
  }
  for (const name of dbFields) {
    const el = form.querySelector(`#${name}`);
    if (!el) continue;
    el.classList.remove('is-err');
    el.removeAttribute('aria-invalid');
    const described = (el.getAttribute('aria-describedby') ?? '').split(' ').filter((id) => id && id !== `${name}-dberr`);
    if (described.length) {
      el.setAttribute('aria-describedby', described.join(' '));
    } else {
      el.removeAttribute('aria-describedby');
    }
  }
}

function markDbField(form, name, hint) {
  const el = form.querySelector(`#${name}`);
  if (!el) return null;
  el.classList.add('is-err');
  el.setAttribute('aria-invalid', 'true');
  if (hint) {
    const msg = document.createElement('div');
    msg.className = 'ds-err-msg';
    msg.id = `${name}-dberr`;
    msg.append(iconFrom(form, 'err'));
    const text = document.createElement('span');
    text.textContent = hint;
    msg.append(text);
    el.closest('.ds-field').append(msg);
    const described = (el.getAttribute('aria-describedby') ?? '').split(' ').filter(Boolean);
    el.setAttribute('aria-describedby', [...described, msg.id].join(' '));
  }
  return el;
}

// initDbTest wires Test connection, Test again and Use SQLite instead.
function initDbTest(form, progress) {
  const banner = form.querySelector('[data-ins-db-error]');
  const testRow = form.querySelector('[data-ins-db-test-row]');
  const errorActions = form.querySelector('[data-ins-db-error-actions]');
  const status = form.querySelector('[data-ins-db-status]');
  if (!banner || !status) return;

  const showError = (head, message) => {
    banner.querySelector('[data-ins-db-error-head]').textContent = head;
    banner.querySelector('[data-ins-db-error-msg]').textContent = message;
    banner.hidden = false;
    errorActions.hidden = false;
    testRow.hidden = true;
  };
  const hideError = () => {
    banner.hidden = true;
    errorActions.hidden = true;
    testRow.hidden = false;
  };
  const setStatus = (text, ok) => {
    status.replaceChildren();
    if (ok) {
      status.classList.add('ds-c-ok');
      status.append(iconFrom(form, 'ok'));
    } else {
      status.classList.remove('ds-c-ok');
    }
    if (text) {
      const span = document.createElement('span');
      span.textContent = text;
      status.append(span);
    }
  };
  const sqliteSelected = () => fieldValue(form, 'db_type') === 'sqlite3';
  const syncSqliteButton = () => {
    const btn = form.querySelector('[data-ins-sqlite]');
    if (btn) btn.hidden = sqliteSelected();
  };

  const runTest = async (button) => {
    const buttons = Array.from(form.querySelectorAll('[data-ins-db-test]'));
    for (const b of buttons) b.disabled = true;
    setStatus(status.getAttribute('data-testing'), false);
    const data = new URLSearchParams();
    for (const name of dbFields) data.set(name, fieldValue(form, name));
    const signature = dbSignature(form);
    let result = null;
    try {
      const resp = await POST(button.getAttribute('data-url'), {data, headers: {'X-Asc-Install': '1'}});
      result = await resp.json();
    } catch {
      result = {ok: false, head: status.getAttribute('data-fail-head'), message: status.getAttribute('data-unreachable')};
    }
    for (const b of buttons) b.disabled = false;
    clearDbFieldErrors(form);
    if (result.ok) {
      progress.state.okSignature = signature;
      progress.state.version = result.version ?? '';
      hideError();
      setStatus(result.message, true);
    } else {
      progress.state.okSignature = null;
      progress.state.version = '';
      setStatus('', false);
      showError(result.head ?? status.getAttribute('data-fail-head'), result.message);
      const field = result.field ? markDbField(form, result.field, result.hint) : null;
      (field ?? banner).focus();
    }
    progress.update();
  };

  for (const btn of form.querySelectorAll('[data-ins-db-test]')) {
    btn.addEventListener('click', () => runTest(btn));
  }

  form.querySelector('[data-ins-sqlite]')?.addEventListener('click', () => {
    const type = form.querySelector('#db_type');
    type.value = 'sqlite3';
    type.dispatchEvent(new Event('change', {bubbles: true}));
    clearDbFieldErrors(form);
    hideError();
    setStatus('', false);
    form.querySelector('#db_path')?.focus();
  });

  form.addEventListener('input', (e) => {
    if (dbFields.includes(e.target.id) && progress.state.okSignature !== dbSignature(form)) {
      setStatus('', false);
    }
  });
  form.querySelector('#db_type').addEventListener('change', () => {
    syncSqliteButton();
    if (progress.state.okSignature !== dbSignature(form)) setStatus('', false);
  });
  syncSqliteButton();

  // a failed install renders the error card: put focus on the field it names
  if (!banner.hidden) {
    (form.querySelector('#ins-db [aria-invalid="true"]') ?? banner).focus();
  }
}

function stepOf(el) {
  return Number(el.closest('[data-ins-step]')?.getAttribute('data-ins-step') ?? 1);
}

// initPhoneStepper shows one section per screen under 768 px, with a progress bar, Back and Next.
function initPhoneStepper(form) {
  const bar = form.querySelector('[data-ins-mstep]');
  const nav = form.querySelector('[data-ins-mnav]');
  if (!bar || !nav || !window.matchMedia) return;
  const query = window.matchMedia(phoneQuery);
  const parts = Array.from(form.querySelectorAll('[data-ins-step]'));
  const back = nav.querySelector('[data-ins-back]');
  const next = nav.querySelector('[data-ins-next]');
  const label = bar.querySelector('[data-ins-mstep-label]');
  const progressBar = bar.querySelector('[data-ins-mstep-bar]');

  const firstError = form.querySelector('[aria-invalid="true"], [data-ins-db-error]:not([hidden])');
  let current = firstError ? stepOf(firstError) : 1;

  const apply = () => {
    const phone = query.matches;
    form.classList.toggle('ins-phone', phone);
    bar.hidden = !phone;
    nav.hidden = !phone;
    for (const part of parts) {
      part.classList.toggle('ins-off', phone && Number(part.getAttribute('data-ins-step')) !== current);
    }
    if (!phone) return;
    label.textContent = bar.getAttribute('data-label').replace('{n}', String(current)).replace('{total}', String(stepCount));
    progressBar.value = current;
    back.hidden = current === 1;
    next.hidden = current === stepCount;
    next.textContent = next.getAttribute(current === stepCount - 1 ? 'data-next-review' : 'data-next');
  };

  const go = (step) => {
    current = Math.min(Math.max(step, 1), stepCount);
    apply();
    const heading = parts.find((p) => Number(p.getAttribute('data-ins-step')) === current && p.querySelector('h2'))?.querySelector('h2');
    if (heading) {
      heading.setAttribute('tabindex', '-1');
      heading.focus();
    }
  };

  back.addEventListener('click', () => go(current - 1));
  next.addEventListener('click', () => {
    const fields = parts.filter((p) => Number(p.getAttribute('data-ins-step')) === current).flatMap((p) => Array.from(p.querySelectorAll('input, select')));
    const invalid = fields.find((el) => !el.checkValidity());
    if (invalid) {
      invalid.reportValidity();
      return;
    }
    go(current + 1);
  });
  query.addEventListener?.('change', apply);
  apply();
}

function runState(i, done) {
  if (i < done) return 'done';
  return i === done ? 'now' : 'todo';
}

// setRun shows the install progress run (templates/install_progress.tmpl) with its first `done` steps done and the
// next one running.
export function setRun(run, done) {
  const steps = Array.from(run.querySelectorAll('[data-ins-run-step]'));
  const total = steps.length;
  const label = run.getAttribute('data-label') ?? '';
  run.querySelector('[data-ins-run-label]').textContent = label.replace('{n}', String(Math.min(done + 1, total))).replace('{total}', String(total));
  const bar = run.querySelector('[data-ins-run-bar]');
  bar.max = total * 2;
  bar.value = Math.min(done * 2 + 1, total * 2);
  for (const [i, li] of steps.entries()) {
    const state = runState(i, done);
    li.classList.toggle('is-done', state === 'done');
    li.classList.toggle('is-cur', state === 'now');
    li.querySelector('[data-ins-run-state]').textContent = run.getAttribute(`data-t-${state}`) ?? '';
  }
}

const statusEvery = 500;

// initRunning swaps the form for the install progress once the browser sends it, and moves the progress on with the
// answers of the status route until the hub answers the form.
function initRunning(form) {
  const card = form.querySelector('[data-ins-running]');
  if (!card) return;
  const run = card.querySelector('[data-ins-run]');
  form.addEventListener('submit', (e) => {
    if (e.defaultPrevented) return;
    hideElem(form.querySelectorAll('.ins-split'));
    card.hidden = false;
    setRun(run, 0);
    card.querySelector('h2')?.focus();
    const url = card.getAttribute('data-url');
    setInterval(async () => {
      try {
        const resp = await GET(url);
        if (resp.ok) setRun(run, Number((await resp.json()).done) || 0);
      } catch {}
    }, statusEvery);
  });
}

// initPostInstall shows the waiting state (the install progress, at the merge queue) until the new hub answers, then
// the ready state.
function initPostInstall(page) {
  const card = page.querySelector('[data-ins-poll]');
  if (!card) return;
  const wait = card.querySelector('[data-ins-wait]');
  const ready = card.querySelector('[data-ins-ready]');
  const run = wait.querySelector('[data-ins-run]');
  wait.hidden = false;
  ready.hidden = true;
  if (run) setRun(run, Number(run.getAttribute('data-done')));

  const targetUrl = card.getAttribute('data-ins-poll');
  let tid = setInterval(async () => {
    try {
      const resp = await GET(targetUrl);
      if (tid && resp.status === 200) {
        clearInterval(tid);
        tid = null;
        if (run) setRun(run, Number(run.querySelectorAll('[data-ins-run-step]').length));
        wait.hidden = true;
        ready.hidden = false;
        ready.querySelector('h1')?.focus();
      }
    } catch {}
  }, 1000);
}
