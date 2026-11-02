// Big pull requests load on their own: the next page of files (`a#diff-show-more-files`) and
// each large file's diff (`a.diff-load-button`) are fetched as they come within a few screens of
// the viewport, so nobody clicks "show more". The buttons stay for keyboard users and for
// browsers without IntersectionObserver. Returns a function that stops watching.
export function watchDiffAutoLoad(root, {onMore, onFile}, Observer = window.IntersectionObserver) {
  if (!root || !Observer) return () => {};
  const io = new Observer((entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue;
      io.unobserve(entry.target);
      if (entry.target.id === 'diff-show-more-files') {
        onMore(entry.target.getAttribute('data-href'));
      } else {
        onFile(entry.target);
      }
    }
  }, {rootMargin: '2000px 0px'});
  const watch = () => {
    for (const el of root.querySelectorAll('a#diff-show-more-files, a.diff-load-button')) {
      if (!el.hasAttribute('data-auto-watched')) {
        el.setAttribute('data-auto-watched', '');
        io.observe(el);
      }
    }
  };
  watch();
  const mo = new MutationObserver(watch);
  mo.observe(root, {childList: true, subtree: true});
  return () => {
    mo.disconnect();
    io.disconnect();
  };
}
