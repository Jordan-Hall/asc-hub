# Bring your own storage and AI keys

A person, an organization or a single repository can keep its objects in an S3-compatible bucket it owns, and can store API keys of AI providers that only its own agents, workflows and semantic features use. The hub shows the settings pages and checks who may open them. `asc serve` does the rest: it validates the address, writes the probe object, encrypts the secrets, keeps the audit entries and enforces the spending cap.

Status: the hub side is built and tested against a stand-in for `asc serve`. The pages need an `asc serve` that implements the `Storage` and `AiProviders` services (features `storage.v1` and `ai-keys.v1` in `Version.features`). Without them the pages say so and change nothing.

## Pages

| Level | Storage | AI keys | Who may open it |
| --- | --- | --- | --- |
| Account | `/user/settings/asc/storage` | `/user/settings/asc/ai` | the signed-in user, for their own account |
| Organization | `/org/<org>/settings/asc/storage` | `/org/<org>/settings/asc/ai` | owners of the organization (and site administrators) |
| Repository | `/<owner>/<repo>/settings/asc/storage` | `/<owner>/<repo>/settings/asc/ai` | administrators of a repository stored in asc |

All of them need `[asc] ENABLED = true`; otherwise every route answers 404. The navigation entries appear in the same three settings menus.

### Storage

1. **Add a target.** Name, endpoint, region, bucket, optional folder (prefix), addressing style, access key id and secret access key, and optionally a session token for temporary credentials.
2. **Verify.** `asc serve` resolves the host name, checks the address, connects with TLS, then writes a small probe object, reads it back and deletes it. The page lists every step with its time. A target that fails stays unused; verification is limited to a few tries per hour.
3. **Enable** a verified target, or **disable** it again. Changing the address, bucket, folder or credentials sets it back to "Not verified".
4. **Remove** a target. This never deletes anything in the bucket, and asc refuses while a repository still uses the target.

On a repository page the targets of the repository itself and, when the signed-in user also administers the owner, the targets of the owner are listed. Two actions apply to an enabled target:

- **Use this target** sends new objects of the repository there. Objects already stored stay where they are.
- **Start migration** copies every object, reads each back and compares its hash, switches the repository over and keeps the old copy unless "Delete the old copy afterwards" is ticked and confirmed. Reads and writes keep working during the copy; a failed or cancelled migration leaves the old storage in use. The page shows the state and the share of objects copied, refreshes itself every two seconds while the migration runs, and offers "Cancel migration" until the switch begins.

The instance default (the operator's own S3 setting) is always available as "The server default".

### AI keys

1. **Add a key.** Label, provider (Anthropic, OpenAI, Google or an OpenAI-compatible service), the API key, an optional custom https address (required for an OpenAI-compatible service), an optional default model and an optional monthly cap in US dollars.
2. **Test.** `asc serve` sends one tiny request to the provider. An untested key is not used. The page shows the result, the latency and a fixed provider status (accepted, rejected, rate limited, unreachable, address not allowed, answer not understood).
3. **Details** show the models the key can use, this month's usage by model and by day (requests, tokens, cost) and the spend against the cap. A key at its cap is paused until the month ends or the cap is raised.
4. **Disable**, **enable** and **remove** a key. Asc refuses removal while a live run or token uses it.

## What is protected, and where

- **Secrets are write-only.** The access key id, secret access key, session token and API keys are fields of type password with no autofill, are never prefilled, are sent to asc and dropped. After a failed submit the page is shown again with them empty. Asc returns a hint only: the start and end of an access key id ("AKIA...WXYZ") and the last four characters of an API key. Asc encrypts them at rest under its master key.
- **No secret in text.** The hub does not log a value from a form. A failure shows a fixed, translated sentence for its kind; text that asc wrote for the person is shown only after it was cut, stripped of control characters and escaped, and it is dropped whole if it repeats a value that was just submitted. The hub's log line for a change names the hub user, the action, the scope and the id.
- **Authorization fails closed, twice.** The route group checks Forgejo's permission. The level code (`routers/web/{user,org,repo}/setting/asc_storage.go`) checks again and builds the scope from the route, never from a form value, so a request cannot name another account, organization or repository. Every call that names a target, key or migration by id reads it first (`services/ascbyo`) and answers "not found" unless it belongs to the scope, because the hub calls asc with its internal token (a browser session carries no asc token). Another owner's identifier looks exactly like a missing one and nothing is sent to asc.
- **Network guard.** The hub does not resolve or connect to the address a person enters. It only checks the shape (an http or https URL without user name, password, query or fragment) and asc applies the real guard: https only unless the operator allows loopback http, every resolved address must be public, and the connection goes to the address that was checked, so DNS rebinding does not help. The reason for a refusal (localhost, private network, link-local, cloud metadata, plain http, credentials in the address, unresolvable host, port) is shown to the person.
- **CSRF and caching.** Every form carries the CSRF token. All responses of these pages are sent with `Cache-Control: no-store`. The add, edit, assign and migrate forms carry an idempotency id, so submitting one of them twice is one change.
- **Audit.** Asc writes the audit entry of every change (`storage.target.*`, `storage.migration.*`, `ai.key.*`) in the same transaction as the change.

## Operating notes

- `[storage] allow_user_targets = false` in the asc configuration turns the per-user targets off; the pages then show the refusal from asc.
- `[storage] allow_loopback_http` lets an operator test against a local S3 double; it never applies to other addresses.
- The master key file and its rotation belong to `asc serve` (`[vault] key_file`). The hub holds no key.
- The hub shows which migration is current from the migration list of asc. Asc does not report which target a repository uses today, so the repository page lists the usable targets and the migration history rather than marking one as "current".

## Tests

`services/ascbyo` has unit tests for the input rules, the owner check of every id-based call, error mapping, secret scrubbing and every page text key, against an in-process stand-in for the two services (`services/ascbyo/ascbyotest`). `routers/web/shared/byoweb` renders the real templates with the real English texts for the account and repository levels and covers adding, invalid input, blocked addresses, verification (passing and failing), enable, disable, edit, removal, assignment, migration with progress and cancel, AI key tests, usage, escaping of hostile text, other owners' identifiers, and the absence of every planted secret from every page. The per-level packages test the owner and administrator checks.
