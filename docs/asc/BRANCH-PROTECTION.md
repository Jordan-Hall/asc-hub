# Branch protection is the asc policy

For a repository stored in asc, the rules of the default branch are its policy file, `.asc/policy.toml`. The policy
is reviewed like code and enforced by asc, which reads the copy on the trunk tip before an update, so a change cannot
lift the rules that bind it. The hub's branch protection settings and API are a second way to read and write the same
file. Nothing is stored in the hub's `protected_branch` table for an asc repository. A repository on git keeps the
behaviour of Forgejo.

## Reading

`GET /repos/{owner}/{repo}/branch_protections` and `GET .../branch_protections/{name}` derive the rule from the policy
in force. There is one rule, for the default branch. The settings page "Branches" shows the same values (board
RepoSettingsRules) and links to the policy editor.

## Writing

`POST`, `PATCH` and `DELETE` on `branch_protections` edit the policy text and propose it with `Policy.Propose`. The
answer is `202 Accepted` with the change, the link of its pull request and the protection as proposed. The rule takes
effect when the change lands, after review by a maintainer, like any other change to the policy. `200` with
"nothing changed" means the policy already says this. The web form of Forgejo for a rule redirects to the policy
page.

## The mapping

| Forgejo rule | asc policy |
| --- | --- |
| `required_approvals` | `[trunk] require_approvals` |
| `enable_status_check` and `status_check_contexts` | `[trunk] require_checks` (an empty list when off) |
| `require_signed_commits` | `[signing] require_signed` |
| force push (not part of the API options; kept as it is) | `[trunk] allow_force` |
| `enable_push = false` | `[merge] queue = true`: trunk takes landings only through the queue |
| `protected_file_patterns` (`;` separated) | one `[paths."<glob>"] write = ["role:maintainer"]` per pattern |

Deleting the rule sets approvals to 0, checks to none, signing off, the queue off, and removes the path rules that
this mapping made. A path rule with other writers is never touched or removed.

## Limits

A setting with no asc equivalent is refused with `422` and a sentence naming it. It is never dropped silently:

- a rule for a branch other than the default branch (asc protects trunk only);
- push, merge, approvals and force push whitelists of people, teams or deploy keys (asc decides who may land by
  roles in the policy, not by lists on a rule);
- block on rejected reviews, block on official review requests, block on outdated branch, dismiss stale approvals;
- unprotected file patterns;
- status checks enabled without a context.

`apply_to_admins` is ignored: the asc policy applies to everyone, and only a maintainer of the policy may write a
maintainer-only path.

## Enforcement

A direct push to trunk is refused by asc with `POLICY_DENIED` when the policy needs approvals or checks that the push
does not have; the landing of a change is refused for the same reason. The hub never decides this itself.
