// Deterministic seed for the pixel harness: builds the cast of the design boards (Maya, Priya, Diego, the
// acme organisation, acme/payments with its stacks, issues and releases) through the hub's public REST API.
// Commit dates are fixed, so commit hashes do not change between runs. Everything else (issue and comment times)
// is shifted by backdate() so the relative times match the boards.
//   node tests/pixel/seed.mjs            seeds PIXEL_HUB_URL (default http://127.0.0.1:3610)
import {mkdirSync, writeFileSync} from 'node:fs';
import {dirname} from 'node:path';
import {pathToFileURL} from 'node:url';
import {ADMIN, PASSWORD, HUB_URL, STATE_FILE, ANCHOR} from './config.mjs';

const auth = (user) => `Basic ${Buffer.from(`${user}:${PASSWORD}`).toString('base64')}`;

async function api(method, path, body, {user = ADMIN, sudo, ok = [200, 201, 204, 409, 422]} = {}) {
  const url = `${HUB_URL}/api/v1${path}${sudo ? `${path.includes('?') ? '&' : '?'}sudo=${sudo}` : ''}`;
  const res = await fetch(url, {
    method,
    headers: {Authorization: auth(user), 'Content-Type': 'application/json', Accept: 'application/json'},
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  let json;
  try { json = text ? JSON.parse(text) : null; } catch { json = text; }
  if (!ok.includes(res.status)) throw new Error(`${method} ${path} -> ${res.status} ${text.slice(0, 300)}`);
  return json;
}
const b64 = (s) => Buffer.from(s).toString('base64');
const at = (minutesBeforeAnchor) => new Date(ANCHOR.getTime() - minutesBeforeAnchor * 60_000).toISOString();

const PEOPLE = [
  {u: 'maya', name: 'Maya Okafor', loc: 'Lisbon', bio: 'Payments platform lead. Reviews most of acme/payments-core and keeps the retry policy honest.', site: ''},
  {u: 'priya', name: 'Priya Nair', loc: 'London', bio: 'Retries, idempotency and anything that touches money. Happy to review billing changes.', site: 'priya.dev'},
  {u: 'diego', name: 'Diego Alvarez', loc: 'Madrid', bio: 'Refunds and provider failover.', site: ''},
  {u: 'hannah', name: 'Hannah Weiss', loc: 'Berlin', bio: 'Ledger exports and docs.', site: ''},
  {u: 'tomas', name: 'Tomás Rivera', loc: 'Madrid', bio: 'Ledger and reconciliation. Prefers small stacks and long commit messages.', site: ''},
  {u: 'ines', name: 'Ines Duarte', loc: '', bio: '', site: ''}, // a fresh account with no repositories (Get started)
];

const README = `# payments

The payment service for Acme. Card, bank transfer and refund flows, with provider failover and idempotent retries.

## Run it locally

\`\`\`
$ asc clone hub.acme.dev/acme/payments
$ cd payments && cargo run -p server
# the server listens on :8080 and talks to the sandbox provider
\`\`\`

## How we ship

Work happens in stacks of small changes. Each change says who made it and why, gets reviewed on its own, and lands in order through the merge queue. Agents write in src/ and tests/ only; .asc/ needs a maintainer.
`;

const FILES = {
  'README.md': README,
  'AGENTS.md': '# Agents\n\nAgents write in `src/` and `tests/` only. `.asc/` needs a maintainer.\n',
  'Cargo.toml': '[package]\nname = "payments"\nversion = "2.18.0"\nedition = "2021"\n\n[dependencies]\nserde = "1.0.215"\ntokio = "1.40"\n',
  '.asc/policy.toml': '[merge]\nrequire_review = true\nrequire_checks = ["unit", "lint"]\n',
  'src/lib.rs': 'pub mod refunds;\npub mod retry;\n\npub fn version() -> &\'static str {\n    "2.18.0"\n}\n',
  'src/refunds.rs': 'pub struct Refund {\n    pub id: String,\n    pub amount_cents: u64,\n}\n\npub fn refund(r: &Refund) -> Result<(), String> {\n    if r.amount_cents == 0 {\n        return Err("zero amount".into());\n    }\n    Ok(())\n}\n',
  'src/retry.rs': 'pub fn delay_ms(attempt: u32) -> u64 {\n    100 * 2u64.pow(attempt)\n}\n',
  'tests/refunds.rs': '#[test]\nfn refund_rejects_zero() {\n    assert!(payments::refunds::refund(&payments::refunds::Refund { id: "r1".into(), amount_cents: 0 }).is_err());\n}\n',
  'docs/failover.md': '# Failover order\n\n1. primary-card\n2. secondary-card\n3. bank-transfer\n',
};

const STACKS = [
  {branch: 'retry-with-backoff', title: 'Retry every outbound payment call with jittered backoff', body: 'Retry every outbound payment call with jittered backoff. Refunds share the policy.', author: 'priya', file: 'src/retry.rs', text: 'pub fn delay_ms(attempt: u32) -> u64 {\n    let base = 100 * 2u64.pow(attempt);\n    base + jitter(base)\n}\n\nfn jitter(base: u64) -> u64 {\n    base / 4\n}\n', age: 12},
  {branch: 'refund-idempotency', title: 'Idempotency keys on refunds', body: 'Idempotency keys on refunds, stored for 24 h, with a replay test.', author: 'diego', file: 'src/refunds.rs', text: FILES['src/refunds.rs'] + '\npub fn key(r: &Refund) -> String {\n    format!("refund:{}", r.id)\n}\n', age: 60},
  {branch: 'bump-tokio-1-41', title: 'Raise tokio to 1.41', body: 'Raise tokio to 1.41 and drop the unused rt-multi-thread feature gate.', author: 'hannah', file: 'Cargo.toml', text: FILES['Cargo.toml'].replace('1.40', '1.41'), age: 3},
  {branch: 'ledger-export-csv', title: 'Nightly ledger export to CSV', body: 'Nightly ledger export to CSV, streamed so memory stays flat.', author: 'hannah', file: 'docs/export.md', text: '# Ledger export\n\nStreamed, one row at a time.\n', age: 1440},
];
const EXTRA_BRANCHES = ['provider-timeouts', 'failover-order-docs', 'webhook-backoff', 'sandbox-flaky-fix', 'cursor-compat', 'rate-limit-headers', 'fx-rounding', 'audit-export'];

const ISSUES = [
  {title: 'Retries stampede the provider after an outage', labels: ['bug', 'payments'], by: 'priya', body: 'After a provider outage every client retries at the same instant.\n\n```\nretry loop\n```', comments: 7},
  {title: 'Refund endpoint accepts the same key with a different amount', labels: ['bug', 'security'], by: 'diego', body: 'The idempotency key is not bound to the amount.', comments: 4},
  {title: 'Document the failover order for card providers', labels: ['docs'], by: 'hannah', body: 'Write down the order we try providers in.', comments: 2},
  {title: 'Export ledger as CSV from the dashboard', labels: ['feature', 'ledger'], by: 'tomas', body: 'Finance wants a CSV.', comments: 11},
  {title: 'Webhook retries should back off per receiver', labels: ['enhancement'], by: 'maya', body: 'One slow receiver should not delay the rest.', comments: 3},
  {title: 'Timeout in sandbox gateway test is flaky', labels: ['flaky', 'ci'], by: 'priya', body: 'Fails about one run in ten.', comments: 9},
];
const LABELS = {bug: 'd73a4a', security: 'b60205', docs: '0075ca', feature: '0e8a16', flaky: 'fbca04', ci: 'ededed', payments: '125469', ledger: '5319e7', enhancement: 'a2eeef'};

const OTHER_REPOS = [
  ['payments-core', 'Ledger, charges and refunds. The service that moves the money.', false],
  ['web-app', 'Customer dashboard and checkout. Cursor pagination for every list.', false],
  ['infra', 'Terraform, queues and rate limits for every environment.', false],
  ['ledger', 'Append-only ledger and snapshots.', false],
  ['design-system', 'Tokens and components shared by web and mobile.', false],
  ['legacy-batch', 'Nightly settlement jobs. Read only.', false],
];

export async function seed() {
  const state = {seededAt: new Date().toISOString(), anchor: ANCHOR.toISOString(), password: PASSWORD, users: {}, repo: 'acme/payments'};
  // people
  for (const p of PEOPLE) {
    if (p.u !== ADMIN) {
      await api('POST', '/admin/users', {username: p.u, email: `${p.u}@acme.test`, password: PASSWORD, must_change_password: false, full_name: p.name, visibility: 'public'});
    }
    const me = await api('PATCH', `/admin/users/${p.u}`, {login_name: p.u, source_id: 0, full_name: p.name, location: p.loc, description: p.bio, website: p.site, email: `${p.u}@acme.test`});
    const info = await api('GET', `/users/${p.u}`);
    state.users[p.u] = {id: info.id, name: p.name};
    void me;
  }
  // organisations and teams
  await api('POST', '/orgs', {username: 'acme', full_name: 'Acme', description: 'Payments, ledger and the web app.', location: 'London and remote', website: 'https://acme.example', visibility: 'public'});
  await api('POST', '/orgs', {username: 'northwind', full_name: 'Northwind', description: 'Hosted checkout.', visibility: 'public'}, {sudo: 'priya'});
  for (const [name, desc, perm] of [['Payments', 'The payments team', 'write'], ['Platform', 'Platform and infra', 'write'], ['Reviewers', 'Reviewers', 'read'], ['Design', 'Design system', 'read']]) {
    const t = await api('POST', '/orgs/acme/teams', {name, description: desc, permission: perm, includes_all_repositories: true, units: ['repo.code', 'repo.issues', 'repo.pulls', 'repo.releases'], can_create_org_repo: false});
    if (t && t.id) for (const u of ['priya', 'diego', 'hannah', 'tomas']) await api('PUT', `/teams/${t.id}/members/${u}`);
  }
  // repositories
  const repos = [['payments', 'Payment service: cards, transfers, refunds, provider failover. Owned by the payments team.', true], ...OTHER_REPOS.map(([n, d]) => [n, d, false])];
  for (const [name, description, priv] of repos) {
    await api('POST', '/orgs/acme/repos', {name, description, private: priv, auto_init: false, default_branch: 'main', trust_model: 'default'});
  }
  await api('PUT', '/repos/acme/payments/topics', {topics: ['rust', 'payments', 'idempotency', 'failover']});
  await api('PATCH', '/repos/acme/payments', {website: 'https://docs.acme.dev/payments', has_issues: true, has_pull_requests: true, has_releases: true});
  const diego = {name: 'Diego Alvarez', email: 'diego@acme.test'};
  await api('POST', '/repos/acme/payments/contents', {
    branch: 'main', message: 'Add idempotency keys to refunds', author: diego, committer: diego, dates: {author: at(180), committer: at(180)},
    files: Object.entries(FILES).map(([path, c]) => ({operation: 'create', path, content: b64(c)})),
  });
  for (const [name, description] of OTHER_REPOS) {
    await api('POST', `/repos/acme/${name}/contents`, {
      branch: 'main', message: 'Initial commit', author: diego, committer: diego, dates: {author: at(5000), committer: at(5000)},
      files: [{operation: 'create', path: 'README.md', content: b64(`# ${name}\n\n${description}\n`)}],
    });
  }
  await api('PATCH', '/repos/acme/legacy-batch', {archived: true});
  // stacks (branches with a commit, then a pull request each)
  state.prs = {};
  for (const s of STACKS) {
    const who = {name: PEOPLE.find((p) => p.u === s.author).name, email: `${s.author}@acme.test`};
    const exists = await api('GET', `/repos/acme/payments/contents/${s.file}?ref=main`, undefined, {ok: [200, 404]});
    await api('POST', '/repos/acme/payments/contents', {
      branch: 'main', new_branch: s.branch, message: s.title, author: who, committer: who, dates: {author: at(s.age), committer: at(s.age)},
      files: [{operation: exists && exists.sha ? 'update' : 'create', path: s.file, content: b64(s.text), sha: exists && exists.sha}],
    });
    const pr = await api('POST', '/repos/acme/payments/pulls', {head: s.branch, base: 'main', title: s.title, body: s.body}, {sudo: s.author});
    if (pr && pr.number) state.prs[s.branch] = pr.number;
  }
  for (const b of EXTRA_BRANCHES) await api('POST', '/repos/acme/payments/branches', {new_branch_name: b, old_branch_name: 'main'});
  // labels, milestone, issues
  for (const [name, color] of Object.entries(LABELS)) await api('POST', '/repos/acme/payments/labels', {name, color});
  const labelIds = Object.fromEntries((await api('GET', '/repos/acme/payments/labels?limit=50')).map((l) => [l.name, l.id]));
  await api('POST', '/repos/acme/payments/milestones', {title: 'v2.19.0', description: 'The November release', due_on: '2026-11-14T00:00:00Z'});
  state.issues = [];
  for (const i of ISSUES) {
    const created = await api('POST', '/repos/acme/payments/issues', {title: i.title, body: i.body, labels: i.labels.map((l) => labelIds[l]), milestone: 1}, {sudo: i.by});
    if (created && created.number) {
      state.issues.push(created.number);
      for (let n = 0; n < Math.min(i.comments, 3); n++) await api('POST', `/repos/acme/payments/issues/${created.number}/comments`, {body: ['Can reproduce this on 2.18.', 'I have a fix in review.', 'Linking the stack.'][n]}, {sudo: ['maya', 'diego', 'priya'][n]});
    }
  }
  // releases
  await api('POST', '/repos/acme/payments/releases', {tag_name: 'v2.18.0', target_commitish: 'main', name: 'v2.18.0', body: '## Notes\n\nIdempotent refunds and provider failover.'});
  await api('POST', '/repos/acme/payments/releases', {tag_name: 'v2.19.0-rc1', target_commitish: 'main', name: 'v2.19.0 candidate', body: 'Release candidate.', prerelease: true});
  // facts the mapping needs
  const main = await api('GET', '/repos/acme/payments/branches/main');
  state.sha = main.commit.id;
  state.shaShort = main.commit.id.slice(0, 10);
  state.pr1 = state.prs['retry-with-backoff'] || 1;
  state.issue1 = state.issues[0] || 1;
  state.uid = {maya: state.users.maya.id, priya: state.users.priya.id};
  mkdirSync(dirname(STATE_FILE), {recursive: true});
  writeFileSync(STATE_FILE, JSON.stringify(state, null, 2));
  return state;
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  seed().then((s) => console.log(`seeded ${Object.keys(s.users).length} users, main ${s.shaShort}, stacks ${JSON.stringify(s.prs)}`)).catch((e) => { console.error(e); process.exit(1); });
}
