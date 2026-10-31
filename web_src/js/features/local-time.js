// Draws the times the server wrote in UTC (or in its own zone) in the time zone the person chose on their profile page. The
// page carries the zone in <html data-tz="Europe/London">; without it nothing is changed.

const MONTH_LOCALE = 'en-GB'; // the board's "27 Oct 02:00" and "3 Feb 2026" read day first with English month names

function parts(date, timeZone, opts) {
  const out = {};
  for (const p of new Intl.DateTimeFormat(MONTH_LOCALE, {timeZone, hourCycle: 'h23', ...opts}).formatToParts(date)) out[p.type] = p.value;
  return out;
}

/** The text of a time in the given zone: kind "daytime" is "27 Oct 02:00", kind "day" is "3 Feb 2026". */
export function formatLocalTime(iso, kind, timeZone) {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return null;
  if (kind === 'daytime') {
    const p = parts(date, timeZone, {day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit'});
    return `${p.day} ${p.month} ${p.hour}:${p.minute}`;
  }
  if (kind === 'day') {
    const p = parts(date, timeZone, {day: 'numeric', month: 'short', year: 'numeric'});
    return `${p.day} ${p.month} ${p.year}`;
  }
  return null;
}

/** The full time for the tooltip: "2026-10-27 02:00:00 GMT". */
export function formatLocalTitle(iso, timeZone) {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return null;
  const p = parts(date, timeZone, {year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', timeZoneName: 'short'});
  return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}:${p.second} ${p.timeZoneName}`;
}

/** Rewrites one <time data-local> element; returns whether it changed. A zone the browser does not know changes nothing. */
export function applyLocalTime(el, timeZone) {
  try {
    const iso = el.getAttribute('datetime') || '';
    const text = formatLocalTime(iso, el.getAttribute('data-local'), timeZone);
    if (text === null) return false;
    el.textContent = text;
    el.setAttribute('title', formatLocalTitle(iso, timeZone) ?? '');
    return true;
  } catch {
    return false; // RangeError: an unknown time zone
  }
}

export function applyLocalTimes(root, timeZone) {
  let n = 0;
  for (const el of root.querySelectorAll('time[data-local]')) {
    if (applyLocalTime(el, timeZone)) n++;
  }
  return n;
}

export function initLocalTime() {
  const timeZone = document.documentElement.getAttribute('data-tz');
  if (!timeZone) return;
  applyLocalTimes(document, timeZone);
  // fragments that arrive later (htmx, tabs) carry times too
  new MutationObserver((records) => {
    for (const r of records) {
      for (const node of r.addedNodes) {
        if (node.nodeType !== 1) continue;
        if (node.matches?.('time[data-local]')) applyLocalTime(node, timeZone);
        applyLocalTimes(node, timeZone);
      }
    }
  }).observe(document.body, {childList: true, subtree: true});
}
