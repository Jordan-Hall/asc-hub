// Performance budget of the file view: the server time of a page of a large file.
//
//   node tools/asc-fileview-budget.mjs <base url> <user> <password> <path of a file of about 300 KB> [runs] [budget ms]
//
// It signs in, requests the page `runs` times (default 20) and prints p50 and p95 of the time to the last byte of the
// HTML. It exits 1 when p95 is over the budget (default 300 ms). It measures the server (render, highlight, escape,
// the asc round trips) and the transfer, not the browser: the browser's cost of a long file is bounded by the
// highlight window (see modules/highlight/budget.go) and is measured with the trace of a real browser.

const [base, user, password, path, runsArg, budgetArg] = process.argv.slice(2);
if (!base || !user || !password || !path) {
  console.error('usage: node tools/asc-fileview-budget.mjs <base url> <user> <password> <file path> [runs] [budget ms]');
  process.exit(2);
}
const runs = Number(runsArg || 20);
const budget = Number(budgetArg || 300);

let cookies = new Map();
function remember(res) {
  for (const c of res.headers.getSetCookie?.() ?? []) {
    const [pair] = c.split(';');
    const i = pair.indexOf('=');
    cookies.set(pair.slice(0, i), pair.slice(i + 1));
  }
}
const cookieHeader = () => Array.from(cookies, ([k, v]) => `${k}=${v}`).join('; ');

const form = await fetch(`${base}/user/login`, {redirect: 'manual'});
remember(form);
// some builds protect forms with the Sec-Fetch headers and put no token in the page: the token is sent when there is one
const csrf = /name="_csrf" value="([^"]+)"/.exec(await form.text())?.[1];
const login = await fetch(`${base}/user/login`, {
  method: 'POST',
  redirect: 'manual',
  headers: {'content-type': 'application/x-www-form-urlencoded', cookie: cookieHeader()},
  body: new URLSearchParams({...(csrf ? {_csrf: csrf} : {}), user_name: user, password}).toString(),
});
remember(login);

const times = [];
let bytes = 0;
for (let i = 0; i < runs + 2; i++) {
  const t = performance.now();
  const res = await fetch(`${base}${path}`, {headers: {cookie: cookieHeader()}});
  const body = await res.arrayBuffer();
  const ms = performance.now() - t;
  if (res.status !== 200) throw new Error(`status ${res.status} for ${path}`);
  bytes = body.byteLength;
  if (i >= 2) times.push(ms); // the first two runs warm the caches and are not counted
}
times.sort((a, b) => a - b);
const at = (q) => times[Math.min(times.length - 1, Math.floor(times.length * q))];
console.log(`file view ${path}: ${Math.round(bytes / 1024)} KB of HTML, p50 ${at(0.5).toFixed(0)} ms, p95 ${at(0.95).toFixed(0)} ms, budget ${budget} ms`);
process.exit(at(0.95) > budget ? 1 : 0);
