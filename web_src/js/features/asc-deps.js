// The dependencies page (repo/asc_deps). A finding opens in place: its Details button shows why it matters, how the
// package gets in and the fix. Without scripts every finding is drawn open and the buttons stay hidden. The Outdated tile
// asks the registries after the page is drawn (GET asc/deps/outdated), because that takes a while; a failure is said as
// such and never drawn as zero. While the Scan now form waits for its answer, the page asks what the scan has done
// (GET asc/deps/scan/status) every second and draws a bar, the phase and "About N seconds left".

import {GET} from '../modules/fetch.js';

export function outdatedText(sub, counts) {
  const parts = [];
  if (counts.security > 0) parts.push(sub.getAttribute('data-security').replace('%d', String(counts.security)));
  if (counts.unknown > 0) parts.push(sub.getAttribute('data-unknown').replace('%d', String(counts.unknown)));
  return parts.join(' · ');
}

export function initDepToggle(button) {
  const detail = document.querySelector(`#${CSS.escape(button.getAttribute('aria-controls'))}`);
  if (!detail) return;
  detail.hidden = true;
  button.hidden = false;
  button.addEventListener('click', () => {
    const open = button.getAttribute('aria-expanded') !== 'true';
    button.setAttribute('aria-expanded', String(open));
    detail.hidden = !open;
  });
}

export async function loadOutdated(tile) {
  const value = tile.querySelector('[data-deps-outdated-n]');
  const sub = tile.querySelector('[data-deps-outdated-sub]');
  sub.textContent = sub.getAttribute('data-loading');
  try {
    const res = await GET(tile.getAttribute('data-url'));
    if (!res.ok) throw new Error(`outdated: ${res.status}`);
    const counts = await res.json();
    value.textContent = String(counts.outdated);
    sub.textContent = outdatedText(sub, counts);
  } catch {
    value.textContent = '–';
    sub.textContent = sub.getAttribute('data-failed');
  }
}

// The phase line of a scan status: the advisory phase says how many steps of how many.
export function scanPhaseText(el, status) {
  const text = el.getAttribute(`data-${status.phase}`);
  if (!text) return el.textContent;
  if (status.phase !== 'advisories') return text;
  return text.replace('%d', String(status.done)).replace('%d', String(status.total));
}

// The time left of a scan status; empty when there is no estimate.
export function scanEtaText(el, eta) {
  if (!eta) return '';
  if (eta === 1) return el.getAttribute('data-one');
  return el.getAttribute('data-many').replace('%d', String(eta));
}

const scanPollMs = 1000;

export function drawScanStatus(panel, status) {
  if (!status.running) return;
  panel.querySelector('[data-deps-progress-bar]').value = status.permille;
  const phase = panel.querySelector('[data-deps-progress-phase]');
  phase.textContent = scanPhaseText(phase, status);
  const eta = panel.querySelector('[data-deps-progress-eta]');
  eta.textContent = scanEtaText(eta, status.eta);
}

async function pollScan(form, panel) {
  try {
    const res = await GET(form.getAttribute('data-status-url'));
    if (res.ok) drawScanStatus(panel, await res.json());
  } catch {
    // A failed poll keeps what is drawn; the scan itself answers the form.
  }
}

export function initScanProgress(form) {
  const panel = form.querySelector('[data-deps-progress]');
  if (!panel) return;
  form.addEventListener('submit', () => {
    panel.hidden = false;
    form.querySelector('[type="submit"]').disabled = true;
    const timer = setInterval(() => pollScan(form, panel), scanPollMs);
    window.addEventListener('pagehide', () => clearInterval(timer), {once: true});
  });
}

export function initAscDeps() {
  for (const button of document.querySelectorAll('[data-dep-toggle]')) initDepToggle(button);
  const tile = document.querySelector('[data-deps-outdated]');
  if (tile) loadOutdated(tile);
  const form = document.querySelector('[data-deps-scan-form]');
  if (form) initScanProgress(form);
}
