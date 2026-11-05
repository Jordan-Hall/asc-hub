// History filters on a phone (board History): one chip per filter (who, Path, Stack, Date) opens the bottom sheet
// with that filter's part. Path and Stack go to the history of that path or branch; Date goes to the search with
// its after: and before: words, keeping the other words of the search.

const DATE = /^\d{4}-\d{2}-\d{2}$/;

// The path as URL segments: no empty, "." or ".." segment, each one escaped.
export function pathSegments(path) {
  return String(path)
    .split('/')
    .filter((s) => s !== '' && s !== '.' && s !== '..')
    .map((s) => encodeURIComponent(s))
    .join('/');
}

export function pathURL(repoLink, refSub, path) {
  const p = pathSegments(path);
  return `${repoLink}/commits/${refSub}${p ? `/${p}` : ''}`;
}

export function stackURL(repoLink, ref, path) {
  const r = pathSegments(ref);
  if (!r) return '';
  const p = pathSegments(path);
  return `${repoLink}/commits/branch/${r}${p ? `/${p}` : ''}`;
}

export function dateURL(repoLink, refSub, after, before, words) {
  const q = [];
  if (DATE.test(after)) q.push(`after:${after}`);
  if (DATE.test(before)) q.push(`before:${before}`);
  if (words.trim()) q.push(words.trim());
  if (!q.length) return `${repoLink}/commits/${refSub}`;
  return `${repoLink}/commits/${refSub}/search?q=${encodeURIComponent(q.join(' '))}`;
}

export function openPart(dialog, name) {
  let shown = null;
  for (const part of dialog.querySelectorAll('[data-history-part]')) {
    part.hidden = part.getAttribute('data-history-part') !== name;
    if (!part.hidden) shown = part;
  }
  if (!shown) return false;
  dialog.querySelector('[data-history-sheet-h]').textContent = shown.getAttribute('data-title');
  if (!dialog.open) dialog.showModal();
  shown.querySelector('input')?.focus();
  return true;
}

export function initAscHistorySheet() {
  const dialog = document.querySelector('dialog[data-history-sheet]');
  if (!dialog || typeof dialog.showModal !== 'function') return;
  const at = (name) => dialog.getAttribute(name) ?? '';
  for (const chip of document.querySelectorAll('[data-history-chip]')) {
    chip.addEventListener('click', () => openPart(dialog, chip.getAttribute('data-history-chip')));
  }
  const go = (name, url) => {
    dialog.querySelector(`form[data-history-part="${name}"]`)?.addEventListener('submit', (e) => {
      e.preventDefault();
      const target = url(new FormData(e.target));
      if (target) window.location.assign(target);
    });
  };
  go('path', (f) => pathURL(at('data-repo-link'), at('data-ref-sub'), f.get('path') ?? ''));
  go('stack', (f) => stackURL(at('data-repo-link'), f.get('ref') ?? '', at('data-tree-path')));
  go('date', (f) => dateURL(at('data-repo-link'), at('data-ref-sub'), f.get('after') ?? '', f.get('before') ?? '', at('data-words')));
}
