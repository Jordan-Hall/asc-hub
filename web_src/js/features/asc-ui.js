// The download page of the ASC Hub shell: system tabs and the processor of the visitor. The "/" key of the
// header search and the count on the inbox link live in asc-keys.js.

// The processor of a visitor from the browser's client hints. Only the two builds asc ships are named; any
// other answer is null and the page keeps the server's guess.
export function archFromHints(hints) {
  if (!hints || typeof hints.architecture !== 'string') return null;
  const arch = hints.architecture.toLowerCase();
  if (arch === 'arm') return 'arm64';
  if (arch === 'x86') return 'x86_64';
  return null;
}

// The operating system of a visitor from the client hint "platform".
export function osFromPlatform(platform) {
  if (typeof platform !== 'string') return null;
  const p = platform.toLowerCase();
  if (p === 'windows') return 'windows';
  if (p === 'macos') return 'macos';
  if (p === 'linux') return 'linux';
  return null;
}

function selectOS(root, os) {
  const tabs = root.querySelectorAll('[role="tab"][data-os]');
  const panels = root.querySelectorAll('.hub-dl-panel[data-os]');
  let found = false;
  for (const panel of panels) if (panel.getAttribute('data-os') === os) found = true;
  if (!found) return;
  for (const tab of tabs) {
    const on = tab.getAttribute('data-os') === os;
    tab.setAttribute('aria-selected', String(on));
    tab.tabIndex = on ? 0 : -1;
  }
  for (const panel of panels) panel.hidden = panel.getAttribute('data-os') !== os;
}

function recommendArch(root, os, arch) {
  for (const panel of root.querySelectorAll(`.hub-dl-panel[data-os="${os}"]`)) {
    const cards = panel.querySelectorAll('.hub-dl-arch[data-arch]');
    let match = false;
    for (const card of cards) if (card.getAttribute('data-arch') === arch) match = true;
    if (!match) continue;
    for (const card of cards) {
      const on = card.getAttribute('data-arch') === arch;
      card.toggleAttribute('data-recommended', on);
      const badge = card.querySelector('.hub-dl-badge');
      if (badge) badge.hidden = !on;
    }
  }
}

// Turns the stacked system panels into tabs and aims them at the visitor's computer. Without this script
// every panel stays visible, one under the other.
function initDownload() {
  const root = document.querySelector('[data-asc-download]');
  if (!root) return;
  root.classList.add('is-tabbed');
  const bar = root.querySelector('.hub-dl-tabs');
  if (bar) bar.hidden = false;
  let os = root.getAttribute('data-active-os');
  selectOS(root, os);

  const tabs = Array.from(root.querySelectorAll('[role="tab"][data-os]'));
  for (const tab of tabs) {
    tab.addEventListener('click', () => selectOS(root, tab.getAttribute('data-os')));
    tab.addEventListener('keydown', (e) => {
      const i = tabs.indexOf(tab);
      let next = -1;
      if (e.key === 'ArrowRight') next = (i + 1) % tabs.length;
      else if (e.key === 'ArrowLeft') next = (i - 1 + tabs.length) % tabs.length;
      else if (e.key === 'Home') next = 0;
      else if (e.key === 'End') next = tabs.length - 1;
      if (next < 0) return;
      e.preventDefault();
      tabs[next].focus();
      selectOS(root, tabs[next].getAttribute('data-os'));
    });
  }

  const hints = navigator.userAgentData;
  if (hints && typeof hints.getHighEntropyValues === 'function') {
    hints.getHighEntropyValues(['architecture', 'platform']).then((values) => {
      const detectedOS = osFromPlatform(values.platform);
      const arch = archFromHints(values);
      if (detectedOS) {
        os = detectedOS;
        selectOS(root, os);
      }
      if (arch) recommendArch(root, os, arch);
    }).catch(() => {});
  }
}

export function initAscUi() {
  initDownload();
}
