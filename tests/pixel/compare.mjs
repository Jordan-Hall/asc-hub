// The two comparisons: pixels (pixelmatch metric) and structure (headings, buttons, cards and chips matched by
// text), and the plain-language list of the largest differences that comes out of them.
import sharp from 'sharp';
import {pixelmatch} from './pixelmatch.mjs';

const MAX_HEIGHT = 8000;
const PAD = {r: 255, g: 0, b: 255, alpha: 1}; // the colour of "this page has nothing here": counts as a mismatch

async function raw(buf, width, height) {
  const img = sharp(buf).ensureAlpha();
  const meta = await img.metadata();
  const w = Math.min(meta.width, width);
  const h = Math.min(meta.height, height);
  const data = await sharp(buf).ensureAlpha().extract({left: 0, top: 0, width: w, height: h}).extend({right: width - w, bottom: height - h, background: PAD}).raw().toBuffer();
  return data;
}

/** Pixel comparison of two PNG buffers of the same viewport width. Returns the mismatch and a diff image. */
export async function comparePixels(boardPng, livePng, width, maxHeight = MAX_HEIGHT) {
  const [mb, ml] = await Promise.all([sharp(boardPng).metadata(), sharp(livePng).metadata()]);
  const height = Math.min(maxHeight, Math.max(mb.height, ml.height));
  const [a, b] = await Promise.all([raw(boardPng, width, height), raw(livePng, width, height)]);
  const out = Buffer.alloc(width * height * 4);
  const diff = pixelmatch(a, b, out, width, height, {threshold: 0.1});
  const diffPng = await sharp(out, {raw: {width, height, channels: 4}}).webp({quality: 80}).toBuffer();
  // rows (in 100px bands) with the most mismatch, to say where the pages part
  const bands = [];
  for (let y = 0; y < height; y += 100) {
    let n = 0;
    const end = Math.min(height, y + 100);
    for (let r = y; r < end; r++) {
      for (let x = 0; x < width; x++) {
        const p = (r * width + x) * 4;
        if (out[p] === 255 && out[p + 1] === 0 && out[p + 2] === 0) n++;
      }
    }
    bands.push(n / (width * (end - y)));
  }
  return {width, height, boardHeight: mb.height, liveHeight: ml.height, diffPixels: diff, mismatch: diff / (width * height), bands, diffPng};
}

const norm = (s) => s.toLowerCase().replace(/[^\p{L}\p{N}]+/gu, ' ').trim().slice(0, 40);

/** Matches the board's elements to the live page's by kind and text, in document order. */
export function compareStructure(board, live) {
  const pool = new Map();
  for (const e of live.elements) {
    const k = `${e.kind}|${norm(e.key)}`;
    if (!pool.has(k)) pool.set(k, []);
    pool.get(k).push(e);
  }
  const used = new Set();
  const matched = [];
  const missing = [];
  const nearest = (list, e) => list.filter((c) => !used.has(c)).sort((p, q) => Math.abs(p.x - e.x) + Math.abs(p.y - e.y) / 4 - Math.abs(q.x - e.x) - Math.abs(q.y - e.y) / 4)[0];
  const take = (kind, key, e) => {
    const exact = pool.get(`${kind}|${key}`);
    const hit = exact && nearest(exact, e);
    if (hit) return hit;
    if (key.length < 6) return null;
    for (const [k, list] of pool) {
      if (!k.startsWith(`${kind}|`)) continue;
      const lk = k.slice(kind.length + 1);
      if (lk.length >= 6 && (lk.startsWith(key) || key.startsWith(lk))) {
        const h = nearest(list, e);
        if (h) return h;
      }
    }
    return null;
  };
  for (const e of board.elements) {
    const hit = take(e.kind, norm(e.key), e);
    if (hit) { used.add(hit); matched.push({board: e, live: hit}); } else missing.push(e);
  }
  const extra = live.elements.filter((e) => !used.has(e));
  const byKind = {};
  for (const e of board.elements) {
    const k = (byKind[e.kind] ||= {board: 0, matched: 0});
    k.board++;
  }
  for (const m of matched) byKind[m.board.kind].matched++;
  return {boardCount: board.elements.length, liveCount: live.elements.length, matched, missing, extra, byKind, coverage: board.elements.length ? matched.length / board.elements.length : 1};
}

const dist = (a, b) => {
  const p = (h) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
  const [x, y] = [p(a), p(b)];
  return Math.hypot(x[0] - y[0], x[1] - y[1], x[2] - y[2]);
};
const px = (v) => parseFloat(v) || 0;
const label = (e) => `${e.kind} "${e.text.slice(0, 40)}"`;

/** The concrete differences, worst first: missing elements, size and gap changes, colours, fonts, page height. */
export function deltas(boardM, liveM, s, status) {
  const out = [];
  if (status && status !== 200) out.push({score: 5000, type: 'status', text: `live route answers HTTP ${status} (no page to compare with)`});
  if (boardM.pageBg && liveM.pageBg && dist(boardM.pageBg, liveM.pageBg) > 6) {
    out.push({score: dist(boardM.pageBg, liveM.pageBg) * 3, type: 'colour', text: `page background ${boardM.pageBg} on the board, ${liveM.pageBg} live`});
  }
  if (boardM.bodyFamily && liveM.bodyFamily && boardM.bodyFamily !== liveM.bodyFamily) {
    out.push({score: 300, type: 'font', text: `body font ${boardM.bodyFamily} on the board, ${liveM.bodyFamily} live`});
  }
  const dh = liveM.height - boardM.height;
  if (Math.abs(dh) >= 40) out.push({score: Math.min(Math.abs(dh), 900) / 2, type: 'size', text: `page is ${Math.abs(dh)}px ${dh > 0 ? 'taller' : 'shorter'} live (${liveM.height}px against ${boardM.height}px)`});
  const weight = {card: 120, heading: 110, button: 95, chip: 40};
  for (const m of s.missing) {
    out.push({score: 400 + (weight[m.kind] || 30) + Math.min(m.w * m.h / 400, 60), type: 'missing', text: `missing live: ${label(m)} (${m.w}x${m.h}px at ${m.x},${m.y} on the board)`});
  }
  let prevDy = 0;
  for (const {board: b, live: l} of [...s.matched].sort((p, q) => p.board.y - q.board.y || p.board.x - q.board.x)) {
    const dx = l.x - b.x, dw = l.w - b.w, dhh = l.h - b.h, dy = l.y - b.y;
    const gap = dy - prevDy; // movement beyond what the elements above already caused
    prevDy = dy;
    const size = Math.max(Math.abs(dw), Math.abs(dhh));
    if (size >= 6 || Math.abs(dx) >= 6 || Math.abs(gap) >= 12) {
      const parts = [];
      if (Math.abs(dw) >= 6) parts.push(`${Math.abs(dw)}px ${dw > 0 ? 'wider' : 'narrower'} (${l.w} against ${b.w})`);
      if (Math.abs(dhh) >= 6) parts.push(`${Math.abs(dhh)}px ${dhh > 0 ? 'taller' : 'shorter'} (${l.h} against ${b.h})`);
      if (Math.abs(dx) >= 6) parts.push(`${Math.abs(dx)}px ${dx > 0 ? 'right' : 'left'} (x ${l.x} against ${b.x})`);
      if (Math.abs(gap) >= 12) parts.push(`${Math.abs(gap)}px ${gap > 0 ? 'lower' : 'higher'} than the layout above explains (y ${l.y} against ${b.y})`);
      out.push({score: Math.max(size, Math.abs(dx), Math.abs(gap)) * (b.kind === 'card' ? 0.9 : 1.1), type: 'size', text: `${label(b)} live is ${parts.join(', ')}`});
    }
    for (const [prop, name] of [['bg', 'background'], ['color', 'text colour'], ['border', 'border colour']]) {
      if (b[prop] && l[prop] && dist(b[prop], l[prop]) > 14) {
        out.push({score: dist(b[prop], l[prop]) * (b.kind === 'card' || b.kind === 'button' ? 1.2 : 0.8), type: 'colour', text: `${label(b)} ${name}: board ${b[prop]}, live ${l[prop]}`});
      } else if (prop === 'bg' && b.bg && !l.bg && (b.kind === 'button' || b.kind === 'chip')) {
        out.push({score: 60, type: 'colour', text: `${label(b)} background: board ${b.bg}, live none`});
      }
    }
    if (px(b.font) && Math.abs(px(b.font) - px(l.font)) >= 1) {
      out.push({score: 20 + Math.abs(px(b.font) - px(l.font)) * 6, type: 'font', text: `${label(b)} font size: board ${b.font}, live ${l.font}`});
    }
    if (b.weight !== l.weight && b.kind === 'heading') out.push({score: 25, type: 'font', text: `${label(b)} weight: board ${b.weight}, live ${l.weight}`});
    if (px(b.radius) !== px(l.radius) && Math.abs(px(b.radius) - px(l.radius)) >= 3 && (b.kind === 'card' || b.kind === 'button')) {
      out.push({score: 18 + Math.abs(px(b.radius) - px(l.radius)), type: 'shape', text: `${label(b)} corner radius: board ${b.radius}, live ${l.radius}`});
    }
  }
  out.sort((x, y) => y.score - x.score);
  // top three, no more than two of one type
  const top = [];
  const count = {};
  for (const d of out) {
    if ((count[d.type] || 0) >= 2) continue;
    top.push(d);
    count[d.type] = (count[d.type] || 0) + 1;
    if (top.length === 3) break;
  }
  for (const d of out) { if (top.length === 3) break; if (!top.includes(d)) top.push(d); }
  return {top, all: out.slice(0, 40)};
}
