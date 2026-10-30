// The files page of a stacked change (design board Review): the stack rail keeps the scroll position of each change,
// the diff can be filtered by what the files mean or by what changed since the viewer's last review, the "Your review"
// card counts what the viewer has read, and the diff shows a skeleton while more files load and a way out when they do not.
// Every piece does nothing when its markup is not on the page, so the module is safe to start on every page.

const RAIL_PREFIX = 'asc-rail-scroll:';
const FILE_SELECTOR = '.rv-file[data-asc-class]';

// ----------------------------------------------------------------------------------------------- rail scroll

export function railKey(pathname) {
  return `${RAIL_PREFIX}${pathname}`;
}

export function saveRailScroll(storage, pathname, y) {
  try {
    storage.setItem(railKey(pathname), String(Math.max(0, Math.round(y))));
  } catch {
    // storage is full or blocked: the position is not kept, nothing else is affected
  }
}

// Returns the saved position of a change, or null when there is none. The position is taken once.
export function takeRailScroll(storage, pathname) {
  try {
    const raw = storage.getItem(railKey(pathname));
    if (raw === null) return null;
    storage.removeItem(railKey(pathname));
    const y = Number.parseInt(raw);
    return Number.isFinite(y) && y >= 0 ? y : null;
  } catch {
    return null;
  }
}

export function initRailScroll(doc = document, win = window) {
  const rail = doc.querySelector('[data-asc-stack]');
  if (!rail) return;
  let storage;
  try {
    storage = win.sessionStorage;
  } catch {
    return;
  }
  if (!win.location.hash) {
    const y = takeRailScroll(storage, win.location.pathname);
    if (y !== null) win.scrollTo(0, y);
  }
  rail.addEventListener('click', (e) => {
    const link = e.target.closest?.('a.rv-stn-link');
    if (!link || link.closest('.is-current')) return;
    saveRailScroll(storage, win.location.pathname, win.scrollY);
  });
}

// ----------------------------------------------------------------------------------------------- hiding files

// A file is hidden by the class filter, by the "changed since my review" filter, or by both; each keeps its own flag.
function syncHidden(file) {
  file.hidden = file.hasAttribute('data-asc-hide-class') || file.hasAttribute('data-asc-hide-changed');
}

export function fillCount(template, ...numbers) {
  let i = 0;
  return String(template ?? '').replace(/%d/g, () => String(numbers[i++] ?? 0));
}

export function applyClassFilter(root, label) {
  let shown = 0;
  const files = root.querySelectorAll(FILE_SELECTOR);
  for (const file of files) {
    const match = !label || file.getAttribute('data-asc-class') === label;
    file.toggleAttribute('data-asc-hide-class', !match);
    syncHidden(file);
    if (match) shown++;
  }
  return {shown, total: files.length};
}

export function applyChangedOnly(root, on) {
  let shown = 0;
  const files = root.querySelectorAll(FILE_SELECTOR);
  for (const file of files) {
    const changed = Boolean(file.querySelector('.changed-since-last-review'));
    file.toggleAttribute('data-asc-hide-changed', on && !changed);
    syncHidden(file);
    if (!file.hidden) shown++;
  }
  return {shown, total: files.length};
}

// Files that load later ("show more") get the filters that are on.
function watchNewFiles(root, reapply) {
  const boxes = root.querySelector('#diff-file-boxes');
  if (!boxes || typeof MutationObserver === 'undefined') return;
  new MutationObserver(() => reapply()).observe(boxes, {childList: true});
}

export function initClassFilter(root = document) {
  const bar = root.querySelector('[data-asc-classbar]');
  if (!bar) return;
  const status = bar.querySelector('[data-asc-class-status]');
  let current = '';
  const apply = () => {
    const {shown, total} = applyClassFilter(root, current);
    for (const chip of bar.querySelectorAll('[data-asc-class]')) {
      chip.setAttribute('aria-pressed', String(chip.getAttribute('data-asc-class') === current));
    }
    if (status) {
      status.textContent = current ? fillCount(shown === 1 ? bar.dataset.tplOne : bar.dataset.tplOther, shown, total) : '';
    }
  };
  bar.addEventListener('click', (e) => {
    const chip = e.target.closest?.('button[data-asc-class]');
    if (!chip) return;
    const label = chip.getAttribute('data-asc-class');
    current = label === current ? '' : label;
    apply();
  });
  watchNewFiles(root, () => {
    if (current) applyClassFilter(root, current);
  });
}

export function initChangedOnly(root = document) {
  const button = root.querySelector('[data-asc-changed-only]');
  if (!button) return;
  let on = false;
  const apply = () => {
    applyChangedOnly(root, on);
    button.setAttribute('aria-pressed', String(on));
    button.textContent = on ? button.dataset.on : button.dataset.off;
  };
  button.addEventListener('click', () => {
    on = !on;
    apply();
  });
  watchNewFiles(root, () => {
    if (on) applyChangedOnly(root, on);
  });
}

// ----------------------------------------------------------------------------------------------- your review

// Open conversations are the code comment threads that are not resolved.
export function openThreadCount(root) {
  let n = 0;
  for (const holder of root.querySelectorAll('.conversation-holder[data-path]')) {
    if (!holder.querySelector('.resolved-placeholder') && holder.querySelector('.comment-list')) n++;
  }
  return n;
}

export function viewedCounts(root, fallbackViewed, fallbackTotal) {
  const bar = root.querySelector('#viewed-files-summary');
  if (bar) {
    const total = Number(bar.getAttribute('max'));
    const viewed = Number(bar.getAttribute('value') ?? bar.value);
    if (Number.isFinite(total) && Number.isFinite(viewed)) return {viewed, total};
  }
  return {viewed: fallbackViewed, total: fallbackTotal};
}

export function initReviewCard(root = document) {
  const card = root.querySelector('[data-asc-review-card]');
  if (!card) return;
  const filesRow = card.querySelector('[data-asc-you-files]');
  const threadsRow = card.querySelector('[data-asc-you-threads]');
  const intent = card.querySelector('[data-asc-you-intent]');
  const key = `asc-you-intent:${card.dataset.pull ?? ''}`;

  const render = () => {
    if (filesRow) {
      const {viewed, total} = viewedCounts(root, Number(card.dataset.viewed), Number(card.dataset.total));
      filesRow.textContent = fillCount(total === 1 ? card.dataset.tplFilesOne : card.dataset.tplFilesOther, viewed, total);
      filesRow.closest('li')?.classList.toggle('is-done', total > 0 && viewed >= total);
    }
    if (threadsRow) {
      const n = openThreadCount(root);
      threadsRow.textContent = fillCount(n === 1 ? card.dataset.tplThreadsOne : card.dataset.tplThreadsOther, n);
      threadsRow.closest('li')?.classList.toggle('is-done', n === 0);
    }
  };
  render();

  if (intent) {
    try {
      intent.checked = window.localStorage.getItem(key) === '1';
    } catch {
      // the box starts unchecked
    }
    intent.addEventListener('change', () => {
      try {
        window.localStorage.setItem(key, intent.checked ? '1' : '0');
      } catch {
        // not kept
      }
    });
  }

  let timer;
  const later = () => {
    clearTimeout(timer);
    timer = setTimeout(render, 100);
  };
  if (typeof MutationObserver !== 'undefined') {
    const target = root.querySelector('#diff-container') ?? root.body ?? root;
    new MutationObserver(later).observe(target, {childList: true, subtree: true, attributes: true, attributeFilter: ['value', 'max']});
    const bar = root.querySelector('#viewed-files-summary');
    if (bar) new MutationObserver(later).observe(bar, {attributes: true});
  }
  root.addEventListener('change', later);

  // "Request changes" and "Comment" open the review box of the page, where the text is written.
  card.addEventListener('click', (e) => {
    const open = e.target.closest?.('[data-asc-open-review]');
    if (!open) return;
    root.querySelector('.js-btn-review')?.click();
    setTimeout(() => root.querySelector('.review-box-panel textarea')?.focus(), 50);
  });
}

// ----------------------------------------------------------------------------------------------- loading and failure

let retryLoad = null;

export function setDiffLoading(on, root = document) {
  const skeleton = root.querySelector('[data-asc-skeleton]');
  if (!skeleton) return false;
  skeleton.hidden = !on;
  if (on) {
    const error = root.querySelector('[data-asc-diff-error]');
    if (error) error.hidden = true;
  }
  return true;
}

// Shows the failure with "Try again". Returns false when the page has no place for it, so the caller can use a toast.
export function showDiffError(retry, root = document) {
  const error = root.querySelector('[data-asc-diff-error]');
  if (!error) return false;
  retryLoad = retry;
  error.hidden = false;
  return true;
}

export function initDiffState(root = document) {
  root.addEventListener('click', (e) => {
    if (!e.target.closest?.('[data-asc-diff-retry]')) return;
    const retry = retryLoad;
    retryLoad = null;
    const error = root.querySelector('[data-asc-diff-error]');
    if (error) error.hidden = true;
    retry?.();
  });
}

export function initAscReview() {
  initRailScroll();
  initClassFilter();
  initChangedOnly();
  initReviewCard();
  initDiffState();
}
