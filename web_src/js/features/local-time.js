// Draws the times the server wrote in UTC (or in its own zone) in the time zone the person chose on their profile page,
// with the clock and the date order they chose on Settings, Appearance. The page carries them in
// <html data-tz="Europe/London" data-clock="12" data-date-order="mdy">; without any of them nothing is changed.

const MONTH_LOCALE = 'en-GB'; // the board's "27 Oct 02:00" and "3 Feb 2026" read day first with English month names

/** The format of the person: clock "24" (default) or "12", order "dmy" (default), "mdy" or "ymd". */
export const DEFAULT_FORMAT = Object.freeze({clock: '24', order: 'dmy'});

function parts(date, timeZone, opts) {
  const out = {};
  const zone = timeZone ? {timeZone} : {};
  for (const p of new Intl.DateTimeFormat(MONTH_LOCALE, {...zone, hourCycle: 'h23', ...opts}).formatToParts(date)) out[p.type] = p.value;
  return out;
}

function clockText(p, clock) {
  if (clock !== '12') return `${p.hour}:${p.minute}`;
  const h = Number(p.hour);
  const suffix = h < 12 ? 'AM' : 'PM';
  return `${h % 12 || 12}:${p.minute} ${suffix}`;
}

function dayText(p, order, withYear) {
  if (order === 'ymd') {
    const md = `${p.monthNum}-${p.dayPad}`;
    return withYear ? `${p.year}-${md}` : md;
  }
  if (order === 'mdy') return withYear ? `${p.month} ${p.day}, ${p.year}` : `${p.month} ${p.day}`;
  return withYear ? `${p.day} ${p.month} ${p.year}` : `${p.day} ${p.month}`;
}

/** The text of a time in the given zone: kind "daytime" is "27 Oct 02:00", kind "day" is "3 Feb 2026" (by default). */
export function formatLocalTime(iso, kind, timeZone, format = DEFAULT_FORMAT) {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return null;
  if (kind !== 'daytime' && kind !== 'day') return null;
  const p = parts(date, timeZone, {day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit'});
  const n = parts(date, timeZone, {day: '2-digit', month: '2-digit'});
  p.monthNum = n.month;
  p.dayPad = n.day;
  if (kind === 'day') return dayText(p, format.order, true);
  // a day and time leaves the year out, except in year-first order where the date reads as a number
  return `${dayText(p, format.order, format.order === 'ymd')} ${clockText(p, format.clock)}`;
}

/** The full time for the tooltip: "2026-10-27 02:00:00 GMT". */
export function formatLocalTitle(iso, timeZone) {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return null;
  const p = parts(date, timeZone, {year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', timeZoneName: 'short'});
  return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}:${p.second} ${p.timeZoneName}`;
}

/** Rewrites one <time data-local> element; returns whether it changed. A zone the browser does not know changes nothing. */
export function applyLocalTime(el, timeZone, format = DEFAULT_FORMAT) {
  try {
    const iso = el.getAttribute('datetime') || '';
    const text = formatLocalTime(iso, el.getAttribute('data-local'), timeZone, format);
    if (text === null) return false;
    el.textContent = text;
    el.setAttribute('title', formatLocalTitle(iso, timeZone) ?? '');
    return true;
  } catch {
    return false; // RangeError: an unknown time zone
  }
}

export function applyLocalTimes(root, timeZone, format = DEFAULT_FORMAT) {
  let n = 0;
  for (const el of root.querySelectorAll('time[data-local]')) {
    if (applyLocalTime(el, timeZone, format)) n++;
  }
  return n;
}

/** The format on <html>: a value it does not know is the default. */
export function pageFormat(root = document.documentElement) {
  const clock = root.getAttribute('data-clock') === '12' ? '12' : '24';
  const order = ['mdy', 'ymd'].includes(root.getAttribute('data-date-order')) ? root.getAttribute('data-date-order') : 'dmy';
  return {clock, order};
}

export function initLocalTime() {
  const root = document.documentElement;
  const timeZone = root.getAttribute('data-tz');
  if (!timeZone && !root.hasAttribute('data-clock') && !root.hasAttribute('data-date-order')) return;
  const format = pageFormat(root);
  applyLocalTimes(document, timeZone, format);
  // fragments that arrive later (htmx, tabs) carry times too
  new MutationObserver((records) => {
    for (const r of records) {
      for (const node of r.addedNodes) {
        if (node.nodeType !== 1) continue;
        if (node.matches?.('time[data-local]')) applyLocalTime(node, timeZone, format);
        applyLocalTimes(node, timeZone, format);
      }
    }
  }).observe(document.body, {childList: true, subtree: true});
}
