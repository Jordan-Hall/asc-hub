import {GET} from '../modules/fetch.js';
import {initAscDeps, outdatedText, scanEtaText, scanPhaseText} from './asc-deps.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn()}));

function page() {
  document.body.innerHTML = `<a data-deps-outdated data-url="/o/r/asc/deps/outdated" href="/o/r/asc/deps?view=updates">
    <div data-deps-outdated-n>–</div>
    <div data-deps-outdated-sub data-loading="checking" data-security="%d with a security fix" data-unknown="%d could not be checked" data-failed="could not check">Newer versions</div>
  </a>
  <button type="button" hidden aria-expanded="false" aria-controls="asc-dep-detail-7" data-dep-toggle>Details</button>
  <div id="asc-dep-detail-7" data-dep-detail>Why it matters</div>`;
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

test('the outdated line names the security fixes and the unchecked packages', () => {
  const sub = document.createElement('div');
  sub.setAttribute('data-security', '%d with a security fix');
  sub.setAttribute('data-unknown', '%d could not be checked');
  expect(outdatedText(sub, {outdated: 11, security: 3, unknown: 0})).toBe('3 with a security fix');
  expect(outdatedText(sub, {outdated: 11, security: 3, unknown: 2})).toBe('3 with a security fix · 2 could not be checked');
  expect(outdatedText(sub, {outdated: 0, security: 0, unknown: 0})).toBe('');
});

test('a finding starts closed with scripts and the button opens and closes it', () => {
  GET.mockResolvedValue({ok: true, json: () => Promise.resolve({outdated: 0, security: 0, unknown: 0})});
  page();
  initAscDeps();
  const button = document.querySelector('[data-dep-toggle]');
  const detail = document.querySelector('[data-dep-detail]');
  expect(button.hidden).toBe(false);
  expect(detail.hidden).toBe(true);
  button.click();
  expect(button.getAttribute('aria-expanded')).toBe('true');
  expect(detail.hidden).toBe(false);
  button.click();
  expect(button.getAttribute('aria-expanded')).toBe('false');
  expect(detail.hidden).toBe(true);
});

test('the outdated tile fills from the registries', async () => {
  GET.mockResolvedValue({ok: true, json: () => Promise.resolve({outdated: 11, security: 3, unknown: 0})});
  page();
  initAscDeps();
  expect(GET).toHaveBeenCalledWith('/o/r/asc/deps/outdated');
  expect(document.querySelector('[data-deps-outdated-sub]').textContent).toBe('checking');
  await flush();
  expect(document.querySelector('[data-deps-outdated-n]').textContent).toBe('11');
  expect(document.querySelector('[data-deps-outdated-sub]').textContent).toBe('3 with a security fix');
});

test('a failed check is said as such and never drawn as zero', async () => {
  GET.mockResolvedValue({ok: false, status: 502});
  page();
  initAscDeps();
  await flush();
  expect(document.querySelector('[data-deps-outdated-n]').textContent).toBe('–');
  expect(document.querySelector('[data-deps-outdated-sub]').textContent).toBe('could not check');
  GET.mockRejectedValue(new Error('offline'));
  page();
  initAscDeps();
  await flush();
  expect(document.querySelector('[data-deps-outdated-sub]').textContent).toBe('could not check');
});

function scanForm() {
  document.body.innerHTML = `<form data-deps-scan-form data-status-url="/o/r/asc/deps/scan/status">
    <button type="submit">Scan now</button>
    <div role="status" hidden data-deps-progress>
      <progress max="1000" data-deps-progress-bar></progress>
      <span data-deps-progress-phase data-reading="Reading" data-advisories="Checking advisories, %d of %d" data-recording="Recording">Scanning…</span>
      <span data-deps-progress-eta data-one="About 1 second left" data-many="About %d seconds left"></span>
    </div>
  </form>`;
}

test('the phase line counts the advisory steps and the time left reads in seconds', () => {
  scanForm();
  const phase = document.querySelector('[data-deps-progress-phase]');
  const eta = document.querySelector('[data-deps-progress-eta]');
  expect(scanPhaseText(phase, {phase: 'advisories', done: 40, total: 120})).toBe('Checking advisories, 40 of 120');
  expect(scanPhaseText(phase, {phase: 'reading'})).toBe('Reading');
  expect(scanPhaseText(phase, {phase: 'unknown'})).toBe('Scanning…');
  expect(scanEtaText(eta, 0)).toBe('');
  expect(scanEtaText(eta, 1)).toBe('About 1 second left');
  expect(scanEtaText(eta, 20)).toBe('About 20 seconds left');
});

test('a scan shows its progress while the form waits and a failed poll keeps what is drawn', async () => {
  vi.useFakeTimers();
  try {
    scanForm();
    initAscDeps();
    const form = document.querySelector('form');
    const panel = document.querySelector('[data-deps-progress]');
    expect(panel.hidden).toBe(true);
    expect(GET).not.toHaveBeenCalledWith('/o/r/asc/deps/scan/status');
    GET.mockResolvedValue({ok: true, json: () => Promise.resolve({running: true, phase: 'advisories', done: 2, total: 4, permille: 525, eta: 20})});
    form.dispatchEvent(new Event('submit', {cancelable: true}));
    expect(panel.hidden).toBe(false);
    expect(form.querySelector('button').disabled).toBe(true);
    await vi.advanceTimersByTimeAsync(1000);
    expect(GET).toHaveBeenCalledWith('/o/r/asc/deps/scan/status');
    expect(panel.querySelector('progress').value).toBe(525);
    expect(panel.querySelector('[data-deps-progress-phase]').textContent).toBe('Checking advisories, 2 of 4');
    expect(panel.querySelector('[data-deps-progress-eta]').textContent).toBe('About 20 seconds left');
    GET.mockResolvedValue({ok: false, status: 502});
    await vi.advanceTimersByTimeAsync(1000);
    expect(panel.querySelector('[data-deps-progress-eta]').textContent).toBe('About 20 seconds left');
    GET.mockResolvedValue({ok: true, json: () => Promise.resolve({running: false})});
    await vi.advanceTimersByTimeAsync(1000);
    expect(panel.querySelector('progress').value).toBe(525);
  } finally {
    vi.useRealTimers();
  }
});
