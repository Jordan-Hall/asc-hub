// Writes the HTML report: index.html (ranked table of every screen) and detail/<id>.html (side by side,
// overlay, diff image and the element tables).
import {mkdirSync, writeFileSync} from 'node:fs';
import {join} from 'node:path';

const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'}[c]));
const pct = (v) => (v * 100).toFixed(v < 0.1 ? 2 : 1) + '%';

const CSS = `
:root{color-scheme:light dark;--bg:#f6f8fa;--card:#fff;--line:#d8dee6;--text:#0f172a;--mute:#5b6b80;--teal:#125469;--bad:#b91c1c;--warn:#92400e;--ok:#047857}
@media (prefers-color-scheme:dark){:root{--bg:#0e1626;--card:#151f33;--line:#2a3750;--text:#e6edf6;--mute:#9fb0c6;--teal:#3aa8c6;--bad:#f87171;--warn:#fbbf24;--ok:#34d399}}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:14px/1.5 Inter,system-ui,Segoe UI,sans-serif}
main{max-width:1500px;margin:0 auto;padding:24px}h1{font-size:24px;margin:0 0 4px}h2{font-size:16px;margin:28px 0 8px}
a{color:var(--teal)}.mute{color:var(--mute)}.mono{font-family:JetBrains Mono,ui-monospace,Consolas,monospace;font-size:12.5px}
.cards{display:flex;gap:12px;flex-wrap:wrap;margin:16px 0}.card{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:12px 16px;min-width:150px}
.card b{display:block;font-size:22px}table{width:100%;border-collapse:collapse;background:var(--card);border:1px solid var(--line);border-radius:8px}
th,td{padding:8px 10px;border-bottom:1px solid var(--line);text-align:left;vertical-align:top}th{font-size:12px;text-transform:uppercase;letter-spacing:.04em;color:var(--mute);cursor:pointer;white-space:nowrap}
tr:hover td{background:color-mix(in srgb,var(--teal) 6%,transparent)}.bar{height:6px;border-radius:3px;background:var(--line);width:110px;margin-top:4px;overflow:hidden}.bar i{display:block;height:100%;background:var(--bad)}
.chip{display:inline-block;border-radius:999px;padding:1px 8px;font-size:12px;border:1px solid var(--line);margin-right:4px;white-space:nowrap}.chip.bad{color:var(--bad)}.chip.warn{color:var(--warn)}.chip.ok{color:var(--ok)}
ul.d{margin:0;padding-left:16px}ul.d li{margin:0 0 2px}input[type=search]{padding:6px 10px;border:1px solid var(--line);border-radius:6px;background:var(--card);color:var(--text);width:260px}
.tabs button{border:1px solid var(--line);background:var(--card);color:var(--text);padding:6px 12px;border-radius:6px;cursor:pointer;margin-right:6px}.tabs button[aria-pressed=true]{background:var(--teal);color:#fff;border-color:var(--teal)}
.pair{display:grid;grid-template-columns:1fr 1fr;gap:12px;align-items:start}.pair figure,.one figure{margin:0;border:1px solid var(--line);background:var(--card);padding:8px;border-radius:8px}
figure img{max-width:100%;display:block}figcaption{font-weight:600;margin-bottom:6px}
.overlay{position:relative;display:inline-block;border:1px solid var(--line)}.overlay img{display:block;max-width:100%}.overlay img+img{position:absolute;inset:0;mix-blend-mode:difference}
.hidden{display:none}
`;

function pageShell(title, body, script = '') {
  return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>${esc(title)}</title><style>${CSS}</style></head><body><main>${body}</main>${script ? `<script>${script}</script>` : ''}</body></html>`;
}

function statusChip(r) {
  if (!r.ok) return '<span class="chip bad">harness error</span>';
  if (r.status !== 200) return `<span class="chip bad">HTTP ${r.status}</span>`;
  return '<span class="chip ok">200</span>';
}

function indexPage(data) {
  const rs = data.results;
  const ok = rs.filter((r) => r.ok);
  const avg = ok.length ? ok.reduce((a, r) => a + r.mismatch, 0) / ok.length : 0;
  const bucket = (lo, hi) => ok.filter((r) => r.mismatch >= lo && r.mismatch < hi).length;
  const err = rs.filter((r) => r.ok && r.status !== 200).length;
  const rows = rs.map((r, i) => {
    const s = r.structure;
    const tops = (r.top || []).map((t) => `<li>${esc(t)}</li>`).join('');
    return `<tr data-q="${esc(`${r.board} ${r.route} ${r.title} ${r.theme} ${r.viewport}`.toLowerCase())}" data-theme="${r.theme}" data-vp="${r.viewport}">
<td data-v="${i + 1}">${i + 1}</td>
<td><a href="detail/${esc(r.id)}.html"><b>${esc(r.num)} ${esc(r.title)}</b></a><div class="mono mute">${esc(r.board)}${r.note ? ` &middot; ${esc(r.note)}` : ''}</div></td>
<td class="mono">${esc(r.route)}${r.finalPath && r.finalPath !== r.route ? `<div class="mute">&rarr; ${esc(r.finalPath)}</div>` : ''}</td>
<td>${esc(r.viewport)} ${esc(r.theme)}${r.needsAsc ? '<div><span class="chip warn">needs asc</span></div>' : ''}</td>
<td data-v="${r.mismatch}"><b>${pct(r.mismatch)}</b><div class="bar"><i style="width:${Math.min(100, r.mismatch * 100)}%"></i></div></td>
<td data-v="${s ? s.coverage : 0}">${s ? `${s.matched}/${s.boardCount} matched<div><span class="chip ${s.missing ? 'bad' : 'ok'}">${s.missing} missing</span><span class="chip">${s.extra} extra</span></div>` : ''}</td>
<td data-v="${r.status || 0}">${statusChip(r)}${r.ok ? `<div class="mono mute">${r.boardHeight} vs ${r.liveHeight}px</div>` : ''}</td>
<td><ul class="d">${tops}</ul></td></tr>`;
  }).join('\n');
  const skipped = data.skipped.map((s) => `<li><b>${esc(s.num)} ${esc(s.title)}</b> <span class="mono">${esc(s.board)}</span>: ${esc(s.reason)}</li>`).join('');
  const body = `<h1>ASC Hub: design boards against the live hub</h1>
<div class="mute">Generated ${esc(data.generatedAt)} &middot; hub ${esc(data.hub)} &middot; clock frozen at ${esc(data.anchor)} &middot; themes ${esc(data.themes.join(', '))}</div>
<div class="cards"><div class="card"><b>${rs.length}</b>comparisons</div><div class="card"><b>${pct(avg)}</b>mean pixel mismatch</div>
<div class="card"><b>${bucket(0, 0.05)}</b>under 5%</div><div class="card"><b>${bucket(0.05, 0.2)}</b>5% to 20%</div><div class="card"><b>${bucket(0.2, 0.5)}</b>20% to 50%</div><div class="card"><b>${bucket(0.5, 2)}</b>over 50%</div>
<div class="card"><b>${err}</b>live page not HTTP 200</div><div class="card"><b>${data.skipped.length}</b>boards not mappable</div></div>
<p class="mute">Mismatch is the share of pixels that differ (pixelmatch metric, threshold 0.1, anti-aliasing ignored) over the larger of the two page heights; the part of the taller page with no counterpart counts as different. The board is rendered without its "States" blocks. Structure matches headings, buttons, cards and chips by their text.</p>
<p><input type="search" id="q" placeholder="Filter by board, route, theme" aria-label="Filter"> <label><input type="checkbox" id="onlyl"> light only</label> <label><input type="checkbox" id="onlym"> mobile only</label></p>
<table id="t"><thead><tr><th>#</th><th>Screen</th><th>Route</th><th>View</th><th>Pixel mismatch</th><th>Structure</th><th>Live</th><th>Largest differences</th></tr></thead><tbody>${rows}</tbody></table>
<h2>Boards not compared</h2><ul>${skipped || '<li>none</li>'}</ul>`;
  const script = `const t=document.getElementById('t'),q=document.getElementById('q'),L=document.getElementById('onlyl'),M=document.getElementById('onlym');
function f(){for(const r of t.tBodies[0].rows){const ok=r.dataset.q.includes(q.value.toLowerCase())&&(!L.checked||r.dataset.theme==='light')&&(!M.checked||r.dataset.vp==='mobile');r.classList.toggle('hidden',!ok)}}
q.oninput=L.onchange=M.onchange=f;
t.tHead.onclick=e=>{const th=e.target.closest('th');if(!th)return;const i=th.cellIndex,d=th.dataset.d=th.dataset.d==='1'?'-1':'1';const rows=[...t.tBodies[0].rows];rows.sort((a,b)=>{const x=a.cells[i].dataset.v??a.cells[i].textContent,y=b.cells[i].dataset.v??b.cells[i].textContent;const n=!isNaN(x)&&!isNaN(y);return (n?x-y:String(x).localeCompare(y))*d});rows.forEach(r=>t.tBodies[0].append(r))};`;
  return pageShell('ASC Hub pixel report', body, script);
}

function detailPage(r) {
  const base = `../shots/${r.id}`;
  const s = r.structure || {missingList: [], extraList: []};
  const tbl = (rows, cols) => `<table><thead><tr>${cols.map((c) => `<th>${c}</th>`).join('')}</tr></thead><tbody>${rows.join('') || `<tr><td colspan="${cols.length}" class="mute">none</td></tr>`}</tbody></table>`;
  const el = (m) => `<tr><td>${esc(m.kind)}</td><td>${esc(m.text)}</td><td class="mono">${m.w}x${m.h} at ${m.x},${m.y}</td></tr>`;
  const bands = (r.bands || []).map((v, i) => (v > 0.05 ? `<span class="chip ${v > 0.3 ? 'bad' : 'warn'}">${i * 100}-${i * 100 + 100}px ${pct(v)}</span>` : '')).join('');
  const body = `<p><a href="../index.html">&larr; all screens</a></p>
<h1>${esc(r.num)} ${esc(r.title)} <span class="mute">${esc(r.viewport)} ${esc(r.theme)}</span></h1>
<div class="mono mute">${esc(r.board)} &rarr; ${esc(r.route)}${r.finalPath && r.finalPath !== r.route ? ` (redirected to ${esc(r.finalPath)})` : ''} &middot; as ${esc(r.as)} ${statusChip(r)} ${r.needsAsc ? '<span class="chip warn">needs asc backend</span>' : ''}</div>
${r.note ? `<p class="mute">${esc(r.note)}</p>` : ''}
${r.ok ? `<div class="cards"><div class="card"><b>${pct(r.mismatch)}</b>pixel mismatch</div><div class="card"><b>${s.matched}/${s.boardCount}</b>elements matched</div><div class="card"><b>${s.missing}</b>missing live</div><div class="card"><b>${r.boardHeight} / ${r.liveHeight}</b>height board / live</div></div>
<h2>Largest differences</h2><ol>${(r.top || []).map((t) => `<li>${esc(t)}</li>`).join('')}</ol>
<div>${bands ? `<span class="mute">Bands with over 5% mismatch: </span>${bands}` : ''}</div>
<h2>Pictures</h2><div class="tabs"><button data-m="pair" aria-pressed="true">Side by side</button><button data-m="overlay" aria-pressed="false">Overlay</button><button data-m="diff" aria-pressed="false">Diff</button></div>
<div id="pair" class="pair" style="margin-top:12px"><figure><figcaption>Board</figcaption><img loading="lazy" src="${base}-board.webp" alt="board"></figure><figure><figcaption>Live hub</figcaption><img loading="lazy" src="${base}-live.webp" alt="live"></figure></div>
<div id="overlay" class="one hidden" style="margin-top:12px"><p class="mute">Board and live page blended with "difference": black means equal, colour means different.</p><div class="overlay"><img loading="lazy" src="${base}-board.webp" alt=""><img loading="lazy" src="${base}-live.webp" alt=""></div></div>
<div id="diff" class="one hidden" style="margin-top:12px"><figure><figcaption>Red: pixels that differ. Yellow: anti-aliasing. Magenta in the live picture: the board is taller.</figcaption><img loading="lazy" src="${base}-diff.webp" alt="diff"></figure></div>
<h2>All measured differences</h2>${tbl((r.deltas || []).map((d) => `<tr><td>${esc(d.type)}</td><td>${esc(d.text)}</td><td>${d.score}</td></tr>`), ['Type', 'Difference', 'Score'])}
<h2>On the board, not found live</h2>${tbl(s.missingList.map(el), ['Kind', 'Text', 'Box on the board'])}
<h2>Live, not on the board</h2>${tbl(s.extraList.map(el), ['Kind', 'Text', 'Box live'])}` : `<p class="chip bad">${esc(r.error)}</p>`}`;
  const script = `for(const b of document.querySelectorAll('.tabs button'))b.onclick=()=>{for(const x of document.querySelectorAll('.tabs button'))x.setAttribute('aria-pressed',x===b);for(const id of ['pair','overlay','diff'])document.getElementById(id).classList.toggle('hidden',id!==b.dataset.m)}`;
  return pageShell(`${r.board} ${r.theme}`, body, script);
}

export function writeReport(dir, data) {
  mkdirSync(join(dir, 'detail'), {recursive: true});
  writeFileSync(join(dir, 'index.html'), indexPage(data));
  for (const r of data.results) writeFileSync(join(dir, 'detail', `${r.id}.html`), detailPage(r));
}
