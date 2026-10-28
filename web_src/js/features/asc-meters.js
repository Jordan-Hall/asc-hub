// Fills a bar from its data-pct attribute: <span class="..." data-pct="72"></span> gets a width of 72%. The width is
// set through the style object (the strict CSP forbids a style attribute in the markup), one frame after load so the
// CSS transition grows the bar. The value is clamped to 0 to 100; anything else leaves the bar empty.

export function clampPct(value) {
  const n = Number.parseFloat(value);
  if (!Number.isFinite(n)) return 0;
  return Math.min(100, Math.max(0, n));
}

export function fillMeters(root = document) {
  const bars = Array.from(root.querySelectorAll('[data-pct]'));
  const paint = () => {
    for (const bar of bars) bar.style.width = `${clampPct(bar.getAttribute('data-pct'))}%`;
  };
  if (typeof requestAnimationFrame === 'function') requestAnimationFrame(paint);
  else paint();
  return bars.length;
}

export function initAscMeters() {
  fillMeters();
}
