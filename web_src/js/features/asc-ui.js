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
  initAscInstallPills();
  initDocsFilter();
  initStackFilter();
  initStackSearch();
  initRefFilter();
  initDocTabs();
}

// The README / AGENTS.md / License tabs of the repository home: the pressed tab shows its panel and hides the others.
// Arrow keys move between the tabs.
export function initDocTabs(root = document) {
  for (const list of root.querySelectorAll('[data-doc-tabs]')) {
    const tabs = Array.from(list.querySelectorAll('[data-doc-tab]'));
    const select = (tab) => {
      for (const t of tabs) {
        const on = t === tab;
        t.setAttribute('aria-selected', String(on));
        t.tabIndex = on ? 0 : -1;
        const panel = root.querySelector(`[data-doc-panel="${t.getAttribute('data-doc-tab')}"]`);
        if (panel) panel.hidden = !on;
      }
    };
    for (const go of root.querySelectorAll('[data-doc-goto]')) {
      go.addEventListener('click', () => {
        const target = tabs.find((t) => t.getAttribute('data-doc-tab') === go.getAttribute('data-doc-goto'));
        if (target) {
          select(target);
          target.focus();
        }
      });
    }
    for (const [i, tab] of tabs.entries()) {
      tab.addEventListener('click', () => select(tab));
      tab.addEventListener('keydown', (e) => {
        const step = {ArrowRight: 1, ArrowLeft: -1}[e.key];
        if (!step) return;
        const next = tabs[(i + step + tabs.length) % tabs.length];
        select(next);
        next.focus();
        e.preventDefault();
      });
    }
  }
}

// The pills of the Refs page (All, Shared, Stacks, Tags) show the sections of one kind without a page load. The first
// pill shows every section.
export function initRefFilter(root = document) {
  for (const group of root.querySelectorAll('[data-ref-filter]')) {
    const buttons = Array.from(group.querySelectorAll('button[data-ref-show]'));
    for (const btn of buttons) {
      btn.addEventListener('click', () => {
        const want = btn.getAttribute('data-ref-show');
        for (const b of buttons) b.setAttribute('aria-pressed', String(b === btn));
        for (const sec of root.querySelectorAll('[data-ref-kind]')) {
          sec.hidden = want !== 'all' && sec.getAttribute('data-ref-kind') !== want;
        }
      });
    }
  }
}

// The Open / Mine / Agents / Needs me buttons over the stack rows of the repository home and of the Stacks list: a
// button shows the rows whose data attribute says so, without a page load. The pressed button is the only truth of
// the filter; with no match a note says so.
export function initStackFilter(root = document) {
  for (const group of root.querySelectorAll('[data-stack-filter]')) {
    const scope = group.closest('[data-stack-scope]') ?? root;
    const buttons = Array.from(group.querySelectorAll('button[data-stack-show]'));
    const attr = {mine: 'data-mine', agent: 'data-agent', needsme: 'data-needsme'};
    for (const btn of buttons) {
      btn.addEventListener('click', () => {
        const want = btn.getAttribute('data-stack-show');
        for (const b of buttons) b.setAttribute('aria-pressed', String(b === btn));
        for (const row of scope.querySelectorAll('[data-stack-row]')) {
          row.toggleAttribute('data-filter-miss', !(want === 'all' || row.getAttribute(attr[want]) === 'true'));
        }
        refreshStackRows(scope);
      });
    }
  }
}

// Shows the rows no filter and no search hides, and the note when none is left.
function refreshStackRows(scope) {
  let shown = 0;
  for (const row of scope.querySelectorAll('[data-stack-row]')) {
    row.hidden = row.hasAttribute('data-filter-miss') || row.hasAttribute('data-search-miss');
    if (!row.hidden) shown++;
  }
  const none = scope.querySelector('[data-stack-none]');
  if (none) none.hidden = shown !== 0;
}

// The search field over the stack rows: every typed word must appear in the text of the row (name, intent, people).
export function initStackSearch(root = document) {
  const input = root.querySelector('input[data-stack-search]');
  if (!input) return;
  const scope = input.closest('[data-stack-scope]') ?? root;
  input.addEventListener('input', () => {
    const words = input.value.toLowerCase().split(/\s+/).filter(Boolean);
    for (const row of scope.querySelectorAll('[data-stack-row]')) {
      const text = row.textContent.toLowerCase();
      row.toggleAttribute('data-search-miss', !words.every((w) => text.includes(w)));
    }
    refreshStackRows(scope);
  });
}

// The install pills of the landing page: one pressed pill shows its command, the others stay hidden.
export function initAscInstallPills(root = document) {
  for (const group of root.querySelectorAll('[data-ds-install]')) {
    const pills = group.querySelectorAll('[data-install-key][aria-pressed]');
    for (const pill of pills) {
      pill.addEventListener('click', () => {
        const key = pill.getAttribute('data-install-key');
        for (const other of pills) other.setAttribute('aria-pressed', String(other === pill));
        for (const cmd of group.querySelectorAll('[data-install-key]:not([aria-pressed])')) cmd.hidden = cmd.getAttribute('data-install-key') !== key;
      });
    }
  }
}

// The search field of the docs start page narrows the guide cards to those whose title or text has the typed words.
export function initDocsFilter(root = document) {
  const input = root.querySelector('input[data-docs-filter]');
  if (!input) return;
  const cards = Array.from(root.querySelectorAll('.site-doccards > li'));
  const none = root.querySelector('[data-docs-none]');
  const apply = () => {
    const words = input.value.toLowerCase().split(/\s+/).filter(Boolean);
    let shown = 0;
    for (const li of cards) {
      const text = li.textContent.toLowerCase();
      const match = words.every((w) => text.includes(w));
      li.hidden = !match;
      if (match) shown++;
    }
    if (none) none.hidden = shown !== 0;
  };
  input.addEventListener('input', apply);
}
