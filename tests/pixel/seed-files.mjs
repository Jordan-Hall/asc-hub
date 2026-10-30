// What the repository acme/payments holds: its first commit, the history that landed on main, the policy and the workflows.
// The open stacks (seed-asc.mjs, or branches with pull requests without asc) are made on top of the tip of main.
const lines = (...l) => l.join('\n') + '\n';

export const README = `# payments

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

// the policy asc enforces on every update after the one that adds it (the parser is strict: unknown keys are refused)
export const POLICY = `version = 1

[roles]
maintainer = ["user:maya"]

[trunk]
require_approvals = 1
require_checks = ["unit", "lint"]

[agents]
enabled = true
default_scope = ["src/**", "tests/**"]
denied_paths = [".github/**"]

[paths.".asc/**"]
write = ["role:maintainer"]

[merge]
strategy = "rebase"
queue = true
revalidate = true
`;

export const WORKFLOWS = {
  '.asc/workflows/dependency-update.toml': `description = "Propose the weekly dependency updates as a reviewed stack"
max_parallel_runs = 1

[[trigger]]
kind = "schedule"
cron = "0 6 * * 1"

[[trigger]]
kind = "manual"

[[input]]
name = "security_only"
required = false
default = "false"

[budget]
max_changes = 5
max_tokens = 300000
max_cost_micro_usd = 6000000

[[step]]
id = "scan"
kind = "checks"
checks = ["deps-audit"]
timeout_seconds = 300

[[step]]
id = "update"
kind = "agent"
needs = ["scan"]
agent = "coder-2"
prompt = "Update the dependencies that have a newer compatible version. Only security fixes when security_only is {{input.security_only}}."
paths = ["Cargo.toml", "Cargo.lock"]
timeout_seconds = 1200

[[step]]
id = "review"
kind = "approval"
needs = ["update"]
approvers = ["user:priya", "user:diego"]
`,
  '.asc/workflows/changed-src-review.toml': `description = "First-pass agent review of every change that touches src/"

[[trigger]]
kind = "change_opened"
paths = ["src/**"]

[[step]]
id = "read"
kind = "agent"
agent = "reviewer-1"
prompt = "Read change {{change}} and list what a reviewer should look at first."
paths = ["src/**"]

[[step]]
id = "notes"
kind = "agent"
needs = ["read"]
agent = "reviewer-1"
prompt = "Leave at most three review notes on change {{change}}."
`,
  '.asc/workflows/release-notes.toml': `description = "Draft release notes when a tag is created"

[[trigger]]
kind = "manual"

[[step]]
id = "collect"
kind = "command"
command = ["asc", "release", "notes"]

[[step]]
id = "draft"
kind = "agent"
needs = ["collect"]
agent = "docs-1"
prompt = "Write release notes from the intent of each change."
paths = ["docs/**"]

[[step]]
id = "approve"
kind = "approval"
needs = ["draft"]
approvers = ["role:maintainer"]
`,
  '.asc/workflows/nightly-fuzz.toml': `description = "Run the fuzz suite against main"

[[trigger]]
kind = "schedule"
cron = "0 2 * * *"

[[step]]
id = "fuzz"
kind = "command"
command = ["cargo", "fuzz", "run", "refunds"]
`,
  // not valid on purpose: the approver is neither user:<name> nor role:<name>
  '.asc/workflows/hotfix-gate.toml': `description = "Extra approvals for changes on release branches"

[[trigger]]
kind = "change_opened"
branches = ["release/*"]

[[step]]
id = "gate"
kind = "approval"
approvers = ["nobody-knows-this-subject"]
`,
};

const REFUNDS = lines(
  'pub struct Refund {', '    pub id: String,', '    pub amount_cents: u64,', '}', '',
  'pub fn refund(r: &Refund) -> Result<(), String> {', '    if r.amount_cents == 0 {', '        return Err("zero amount".into());', '    }', '    Ok(())', '}');

// 1. Start the payments crate
const START = {
  'README.md': README,
  'AGENTS.md': '# Agents\n\nAgents write in `src/` and `tests/` only. `.asc/` needs a maintainer.\n',
  'Cargo.toml': '[package]\nname = "payments"\nversion = "2.18.0"\nedition = "2021"\n\n[dependencies]\nserde = "1.0.215"\ntokio = "1.40"\n',
  'src/lib.rs': 'pub mod refunds;\npub mod retry;\n\npub fn version() -> &\'static str {\n    "2.18.0"\n}\n',
  'src/refunds.rs': REFUNDS,
  'src/retry.rs': 'pub fn delay_ms(attempt: u32) -> u64 {\n    100 * 2u64.pow(attempt)\n}\n',
  'tests/refunds.rs': '#[test]\nfn refund_rejects_zero() {\n    assert!(payments::refunds::refund(&payments::refunds::Refund { id: "r1".into(), amount_cents: 0 }).is_err());\n}\n',
};

// 2. Add retry policy type
export const LIB_WITH_POLICY = 'pub mod policy;\npub mod refunds;\npub mod retry;\n\npub fn version() -> &\'static str {\n    "2.18.0"\n}\n';
const POLICY_RS_1 = lines(
  'pub struct RetryPolicy {', '    pub max_attempts: u32,', '    pub base_ms: u64,', '}', '',
  'impl Default for RetryPolicy {', '    fn default() -> Self {', '        RetryPolicy { max_attempts: 5, base_ms: 100 }', '    }', '}');

// 4. Backoff with jitter (the change the Changeset boards show: three files)
const POLICY_RS_2 = lines(
  'pub struct RetryPolicy {', '    pub max_attempts: u32,', '    pub base_ms: u64,', '    pub jitter: f32, // 0.0 to 1.0', '}', '',
  'impl Default for RetryPolicy {', '    fn default() -> Self {', '        RetryPolicy { max_attempts: 5, base_ms: 100, jitter: 0.25 }', '    }', '}');
const RETRY_RS_2 = lines(
  'use crate::policy::RetryPolicy;', '',
  'pub fn delay_ms(policy: &RetryPolicy, attempt: u32) -> u64 {', '    let raw = policy.base_ms * 2u64.pow(attempt.min(10));',
  '    let capped = raw.min(30_000);', '    capped - (capped as f32 * policy.jitter * 0.5) as u64', '}');
const RETRY_TEST = lines(
  '#[test]', 'fn jitter_stays_within_bounds() {', '    let p = payments::policy::RetryPolicy::default();',
  '    for a in 0..8 { assert!(payments::retry::delay_ms(&p, a) <= 30_000); }', '}');

// 5. Add idempotency keys to refunds
const REFUNDS_KEYED = REFUNDS + lines('', 'pub fn key(r: &Refund) -> String {', '    format!("refund:{}", r.id)', '}');
const REFUNDS_TEST = START['tests/refunds.rs'] + lines('', '#[test]', 'fn the_key_names_the_refund() {',
  '    let r = payments::refunds::Refund { id: "r1".into(), amount_cents: 100 };', '    assert_eq!(payments::refunds::key(&r), "refund:r1");', '}');

/** The commits that landed on main, oldest first. A commit's age is in minutes before the anchor. */
export const HISTORY = [
  {message: 'Start the payments crate', who: 'maya', age: 30 * 1440, files: START},
  {message: 'Add retry policy type', who: 'priya', age: 7200, files: {'src/policy.rs': POLICY_RS_1, 'src/lib.rs': LIB_WITH_POLICY}},
  {message: 'Document the failover order', who: 'hannah', age: 5800, files: {'docs/failover.md': '# Failover order\n\n1. primary-card\n2. secondary-card\n3. bank-transfer\n'}},
  {message: 'Backoff with jitter', who: 'priya', age: 5700, key: 'jitter', files: {'src/retry.rs': RETRY_RS_2, 'src/policy.rs': POLICY_RS_2, 'tests/retry.rs': RETRY_TEST}},
  {message: 'Add idempotency keys to refunds', who: 'diego', age: 180, files: {'src/refunds.rs': REFUNDS_KEYED, 'tests/refunds.rs': REFUNDS_TEST}},
  // last: the policy binds the updates that come after the one that adds it
  {message: 'Add the merge policy and the workflows', who: 'maya', age: 120, files: {'.asc/policy.toml': POLICY, ...WORKFLOWS}},
];

/** The tree at the tip of main, for the stacks that are made without asc. */
export const TREE = Object.assign({}, ...HISTORY.map((c) => c.files));
