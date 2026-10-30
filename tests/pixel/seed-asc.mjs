// The asc half of the seed (PIXEL_ASC=serve): the stacks, their reviews and the releases with their assets, made the way people
// make them, with the real `asc` client against the running hub and `asc serve`. The REST half (seed.mjs) makes the users, the
// organisation, the repositories, the first commit and the issues.
import {spawnSync} from 'node:child_process';
import {join} from 'node:path';
import {ASC_BIN, ASC_PORT, HUB_URL, PASSWORD, PIXEL_DIR, PORT} from './config.mjs';
import {LIB_WITH_POLICY, TREE} from './seed-files.mjs';

const WIN = process.platform === 'win32';
const toWsl = (p) => (WIN ? p.replace(/^([A-Za-z]):[\\/]/, (_, d) => `/mnt/${d.toLowerCase()}/`).replace(/\\/g, '/') : p);
/** A word for the shell: single-quoted, so no character in it means anything to the shell. */
export const q = (s) => `'${String(s).replace(/'/g, `'\\''`)}'`;

/** Runs a bash script (in WSL on Windows) with the helpers of hub/asc-cli.sh loaded; throws with the output on failure. */
export function runScript(script, label) {
  const env = [`HUB=http://127.0.0.1:${PORT}`, `ASC_URL=http://127.0.0.1:${ASC_PORT}`, `ASC_BIN=${toWsl(ASC_BIN)}`, `PASSWORD=${PASSWORD}`, `WORK=/tmp/pixel-seed-${PORT}`];
  const lib = toWsl(join(PIXEL_DIR, 'hub', 'asc-cli.sh'));
  const body = `rm -rf "$WORK"; mkdir -p "$WORK"; trap 'rm -rf "$WORK"' EXIT\nsource ${q(lib)}\n${script}\n`;
  const [file, argv] = WIN ? ['wsl', ['-e', 'env', ...env, 'bash', '-s']] : ['bash', ['-s']];
  const r = spawnSync(file, argv, {input: body, encoding: 'utf8', maxBuffer: 64 << 20, env: WIN ? process.env : {...process.env, ...Object.fromEntries(env.map((e) => e.split(/=(.*)/s).slice(0, 2)))}});
  if (r.status !== 0) throw new Error(`asc seed step "${label}" failed (${r.status}):\n${(r.stdout || '') + (r.stderr || '')}`.slice(-3000));
  return r.stdout;
}

// ------------------------------------------------------------------------------------------------ a web session
/** A signed-in browser session of the hub (cookie jar plus the CSRF token of a page), for the few settings that have only a form. */
async function webSession(user) {
  const jar = new Map();
  const call = async (path, init = {}) => {
    const res = await fetch(`${HUB_URL}${path}`, {...init, redirect: 'manual', headers: {...(init.headers || {}), Cookie: [...jar].map(([k, v]) => `${k}=${v}`).join('; ')}});
    for (const c of res.headers.getSetCookie()) { const [kv] = c.split(';'); const i = kv.indexOf('='); jar.set(kv.slice(0, i), kv.slice(i + 1)); }
    return res;
  };
  const csrf = (html) => /name="_csrf"[^>]*value="([^"]+)"/.exec(html)?.[1] ?? /value="([^"]+)"[^>]*name="_csrf"/.exec(html)?.[1];
  const page = await (await call('/user/login')).text();
  const login = await call('/user/login', {method: 'POST', headers: {'Content-Type': 'application/x-www-form-urlencoded'}, body: new URLSearchParams({_csrf: csrf(page), user_name: user, password: PASSWORD})});
  if (login.status !== 303) throw new Error(`web login of ${user} -> ${login.status}`);
  return {
    async form(path, fields, from = path, status = 303) {
      const html = await (await call(from)).text();
      const op = /name="op"[^>]*value="([^"]+)"/.exec(html)?.[1] ?? /value="([^"]+)"[^>]*name="op"/.exec(html)?.[1] ?? '';
      const res = await call(path, {method: 'POST', headers: {'Content-Type': 'application/x-www-form-urlencoded'}, body: new URLSearchParams({_csrf: csrf(html), op, ...fields})});
      if (res.status !== status) throw new Error(`POST ${path} -> ${res.status}`);
    },
  };
}

// ------------------------------------------------------------------------------------------------ the cast
const lines = (...l) => l.join('\n') + '\n';
const LIB_WIRED = LIB_WITH_POLICY + lines(
  '', 'pub fn send(call: &str) -> Result<(), String> {', '    let policy = policy::RetryPolicy::default();', '    for attempt in 0..policy.max_attempts {',
  '        let _wait = retry::delay_ms(&policy, attempt);', '        if call.is_empty() { continue; }', '        return Ok(());', '    }', '    Err("exhausted".into())', '}');

/** Each stack: the person, the age of its newest change in minutes before the anchor, a reviewer, and its changes bottom first.
 * `shown` is the change the review boards open. */
export const ASC_STACKS = [
  {key: 'retry-with-backoff', who: 'priya', age: 12, reviewer: 'maya', shown: 0, changes: [
    {title: 'Wire the retry policy into the client', body: 'Every outbound payment call now goes through the retry policy. Refunds share it.', files: {'src/lib.rs': LIB_WIRED}},
    {title: 'Docs and metrics for the retry policy', body: 'How the policy works, and a counter for the calls that gave up.', files: {'docs/retries.md': lines('# Retries', '', 'Every outbound call retries with exponential backoff and jitter.'), 'src/metrics.rs': lines('pub fn retry_exhausted() {', '    // counted by the metrics exporter', '}')}},
  ]},
  {key: 'refund-idempotency', who: 'diego', age: 180, reviewer: 'maya', shown: 0, changes: [
    {title: 'Refund-specific retry policy', body: 'Refunds wait longer between attempts than charges do.', files: {'src/refund_retry.rs': lines('pub const REFUND_BASE_MS: u64 = 400;', '', 'pub fn attempts() -> u32 {', '    3', '}')}},
    {title: 'Replay test for refund retries', body: 'The same key with a different amount is a different request.', files: {'tests/replay.rs': lines(
      '#[test]', 'fn the_key_names_the_refund() {', '    let a = payments::refunds::Refund { id: "r1".into(), amount_cents: 100 };', '    assert_eq!(payments::refunds::key(&a), "refund:r1");', '}')}},
  ]},
  {key: 'bump-tokio-1-41', who: 'hannah', age: 3, reviewer: 'priya', shown: 0, changes: [
    {title: 'Raise tokio to 1.41', body: 'Raise tokio to 1.41 and drop the unused rt-multi-thread feature gate.', files: {'Cargo.toml': TREE['Cargo.toml'].replace('1.40', '1.41')}},
  ]},
  {key: 'ledger-export-csv', who: 'hannah', age: 1440, reviewer: 'tomas', shown: 0, changes: [
    {title: 'Nightly ledger export to CSV', body: 'Nightly ledger export to CSV, streamed so memory stays flat.', files: {'docs/export.md': lines('# Ledger export', '', 'Streamed, one row at a time.')}},
  ]},
];

const RELEASES = [
  {tag: 'v2.17.0', title: 'Provider failover', notes: 'Added: provider failover, tried in the documented order.', channel: 'stable', assets: ['payments-server-x86_64-linux.tar.gz', 'SHA256SUMS']},
  {tag: 'v2.17.2', title: 'Patch: ledger export', notes: 'Fixed: the ledger export dropped the last row of a page.', channel: 'stable', assets: ['payments-server-x86_64-linux.tar.gz', 'SHA256SUMS']},
  {tag: 'v2.18.0-rc.1', title: 'Release candidate', notes: 'Added: a retry policy behind a feature flag.', channel: 'pre-release', assets: ['payments-server-x86_64-linux.tar.gz']},
  {tag: 'v2.18.0', title: 'Retries and idempotent refunds', notes: '## Added\n\n- Idempotency keys on refunds, kept for 24 hours\n- A retry policy with exponential backoff and jitter, shared by charges and refunds\n\n## Changed\n\n- Provider timeouts moved to config\n\n## Fixed\n\n- Double charge when a provider timed out after accepting a request\n', channel: 'stable',
    assets: ['payments-server-x86_64-linux.tar.gz', 'payments-server-aarch64-linux.tar.gz', 'payments-server-aarch64-darwin.tar.gz', 'payments-server.sbom.json', 'SHA256SUMS']},
  {tag: 'v2.19.0-rc1', title: 'v2.19.0 candidate', notes: 'Release candidate.', channel: 'pre-release', assets: ['payments-server-x86_64-linux.tar.gz']},
];
export const RELEASE_TAGS = RELEASES.map((r) => r.tag);

/** Makes the asc half of the seed. `api` and `upload` are the REST helpers of seed.mjs; `people` maps a login to its display name; `at` makes a time. */
export async function seedAsc({api, upload, people, at, state}) {
  // agents are off on a fresh server; the board cast has them on
  const admin = await webSession('maya');
  await admin.form('/admin/asc/agents', {allow_agents: '1', budget_usd: '400'});
  await admin.form('/-/asc/agents/features', {enabled: '1'}, '/user/settings');

  // the people page: agents of the organisation, and a Maintainer (the invitations that have not been answered are put in by backdate.py)
  for (const [name, display, runBy] of [['coder-2', 'coder-2', 'maya'], ['reviewer-1', 'reviewer-1', 'priya']]) {
    await admin.form('/org/acme/settings/asc/agents/new', {name, display_name: display, run_by: runBy, paths: ['src/**', 'tests/**'].join(String.fromCharCode(10))}, '/org/acme/settings/asc/agents');
  }
  await admin.form(`/org/acme/members/action/role?uid=${state.users.tomas.id}&role=maintainer`, {}, '/org/acme/members', 200);

  const logins = [...new Set([...ASC_STACKS.flatMap((s) => [s.who, s.reviewer]), 'maya'])];
  let script = logins.map((u) => `person ${u} ${q(people[u])}`).join('\n') + '\n';
  for (const s of ASC_STACKS) {
    script += `mkdir -p "$WORK/ws-${s.key}"; clone ${s.who} acme/payments "$WORK/ws-${s.key}"; cd "$WORK/ws-${s.key}"\n`;
    s.changes.forEach((c, i) => {
      script += `when ${q(at(s.age + (s.changes.length - 1 - i) * 4))}\n`;
      for (const [path, text] of Object.entries(c.files)) {
        script += `mkdir -p "$(dirname ${q(path)})"; cat >${q(path)} <<'PIXEL_FILE_END'\n${text}PIXEL_FILE_END\n`;
        script += `as ${s.who} add ${q(path)}\n`;
      }
      script += `as ${s.who} commit -m ${q(c.title)} -m ${q(c.body)} >/dev/null\n`;
    });
    script += `as ${s.who} submit -s --publish --reviewer ${s.reviewer} >/dev/null\n`;
  }
  runScript(script, 'stacks');

  // the hub's pull requests are the changes; find them by title
  const open = await api('GET', '/repos/acme/payments/pulls?state=open&limit=50');
  const numberOf = Object.fromEntries(open.map((p) => [p.title, p.number]));
  state.prs = {};
  state.changes = {};
  for (const s of ASC_STACKS) {
    state.changes[s.key] = s.changes.map((c) => numberOf[c.title]);
    if (state.changes[s.key].some((n) => n === undefined)) throw new Error(`a change of ${s.key} has no pull request: ${JSON.stringify(numberOf)}`);
    state.prs[s.key] = state.changes[s.key][s.shown];
  }
  const [wire] = state.changes['retry-with-backoff'];
  const [refundRetry] = state.changes['refund-idempotency'];
  const [tokio] = state.changes['bump-tokio-1-41'];
  const review = (n, user, event, body, comments = []) => api('POST', `/repos/acme/payments/pulls/${n}/reviews`, {event, body, comments}, {sudo: user, ok: [200, 201]});
  await review(wire, 'maya', 'COMMENT', 'Two things before this lands.', [
    {path: 'src/lib.rs', new_position: 12, body: 'Cap the exponent: `2u64.pow(attempt)` overflows after 63 attempts.'},
    {path: 'src/lib.rs', new_position: 14, body: 'This returns on the first call, so nothing is ever retried. Retry on the provider error.'},
  ]);
  await review(wire, 'diego', 'COMMENT', 'Should the jitter be random? A fixed quarter makes every client wait the same.');
  await review(refundRetry, 'maya', 'APPROVED', 'Refunds are rarer, so waiting longer is fine.');
  await review(tokio, 'priya', 'APPROVED', 'No feature gate is used.');

  // the releases are the hub's: a draft with its files, then published, which is what a person does in the browser. The hub writes
  // the tag through asc and tells asc about the release with the size and SHA-256 of each file. (A release made with
  // `asc release create` is only a record in asc: the hub has no tag or row for it, so its Releases page would not list it.)
  for (const r of RELEASES) {
    const made = await api('POST', '/repos/acme/payments/releases', {tag_name: r.tag, target_commitish: 'main', name: r.title, body: r.notes, draft: true, prerelease: r.channel === 'pre-release'}, {ok: [201]});
    for (const a of r.assets) {
      const size = a === 'SHA256SUMS' ? 1100 : a.endsWith('.json') ? 217000 : 1800000 + r.tag.length * 1000;
      await upload(`/repos/acme/payments/releases/${made.id}/assets?name=${encodeURIComponent(a)}`, a, Buffer.alloc(size, 120));
    }
    await api('PATCH', `/repos/acme/payments/releases/${made.id}`, {draft: false}, {ok: [200]});
  }

  // workflows, started by Maya the way she would from a terminal
  runScript([
    `person maya ${q(people.maya)}`,
    'mkdir -p "$WORK/runs"; clone maya acme/payments "$WORK/runs"; cd "$WORK/runs"',
    // a workflow starts only when it is switched on; the nightly fuzz run stays off
    'for w in dependency-update changed-src-review release-notes; do as maya workflow enable $w; done',
    // no run of an agent: a key is used only after the provider has answered it, and the harness has no provider
    'as maya workflow run dependency-update',
    'as maya workflow run release-notes || true',
  ].join('\n'), 'runs');
  return state;
}
