// Branch protection of an asc repository is the asc policy: set it through the API, then push straight to trunk and
// see asc refuse the push with POLICY_DENIED. Run against a live hub with asc serve:
//
//   HUB=http://localhost:3500 REPO=qa/payments TOKEN=<access token> \
//   PUSH_CMD='wsl -u root -e su -s /bin/bash nobody -c "bash /mnt/d/tmp/ui10/push.sh"' node tests/asc-e2e/protection_push.mjs
//
// PUSH_CMD must commit to a clone of the repository and run `asc push origin main`; it prints what asc answered.
// The script exits 1 on the first failed expectation.
import { execSync } from 'node:child_process';

const hub = process.env.HUB ?? 'http://localhost:3500';
const repo = process.env.REPO ?? 'qa/payments';
const token = process.env.TOKEN;
const pushCmd = process.env.PUSH_CMD;
if (!token || !pushCmd) {
  console.error('TOKEN and PUSH_CMD are required');
  process.exit(2);
}
const base = `${hub}/api/v1/repos/${repo}/branch_protections`;
const headers = {Authorization: `token ${token}`, 'content-type': 'application/json'};
let failed = false;
const expect = (name, ok, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${name}${ok ? '' : ` ${detail}`}`);
  if (!ok) failed = true;
};

// 1. what asc cannot express is refused, not dropped
let r = await fetch(base, {method: 'POST', headers, body: JSON.stringify({rule_name: 'main', enable_push: true, enable_push_whitelist: true, push_whitelist_usernames: ['someone']})});
expect('a push whitelist is refused with 422', r.status === 422, String(r.status));

// 2. a valid rule is a proposal under review (202), or already what the policy says (200)
r = await fetch(base, {method: 'POST', headers, body: JSON.stringify({rule_name: 'main', required_approvals: 1, enable_status_check: true, status_check_contexts: ['build']})});
expect('a valid rule is accepted as a proposal', r.status === 202 || r.status === 200, String(r.status));
const body = await r.json();
expect('the answer carries the protection', body.protection?.rule_name === 'main' && body.protection?.required_approvals === 1, JSON.stringify(body).slice(0, 200));

// 3. the rule in force is read back from the policy
r = await fetch(`${base}/main`, {headers});
const rule = await r.json();
expect('the rule in force asks for approvals', r.status === 200 && rule.required_approvals >= 1, JSON.stringify(rule).slice(0, 200));

// 4. a direct push to trunk is refused by asc
let out = '';
try {
  out = execSync(pushCmd, {encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe']});
} catch (e) {
  out = `${e.stdout ?? ''}${e.stderr ?? ''}`;
}
expect('a direct push to trunk is refused with POLICY_DENIED', out.includes('POLICY_DENIED'), out.slice(0, 300));
process.exit(failed ? 1 : 0);
