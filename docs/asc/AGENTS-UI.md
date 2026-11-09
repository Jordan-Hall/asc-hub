# Agents and workflows in the hub

Agents are optional. ASC Hub works for people on its own, and every agent page is behind a switch that each person sets for themselves and that is off until they turn it on. This page says what the hub shows, who may do what, and what the hub needs from `asc serve`.

Status: the hub side is built and tested against a stand-in for `asc serve` (the contract is `asc/v1/agents.proto` and `workflows.proto` in the asc repository, docs/BUILDOUT.md sections 11 to 13). It works against a real server when `asc serve` runs the `Agents`, `AgentTokens` and `Workflows` services and, for the model picker, `AiProviders` and `Policy`. `asc serve` in this release does not implement them yet, so the README lists the area as partial.

## The switch

`/-/asc/agents` ("Your agents" in the user menu) has the switch, stored as the user setting `asc.agent_features`. With it off:

- the agent pages (runs, new task, run page, event feed, tokens) answer 404 as if they did not exist, also to the owner of the repository;
- the actions that start, stop, steer, trigger, approve or switch something answer 404;
- the workflow pages for reading (list, definition, run history, run details) still work, without prompts, without per-step logs and without links to agent runs.

The instance and the repository policy have their own switches (`[agents] enabled` in `asc serve`, `[agents] enabled` in the repository policy). When either is off, asc answers `AGENTS_DISABLED` and the page says so in words instead of failing.

## Pages

| Path (under `/{owner}/{repo}/asc/`) | Needs | What it does |
| --- | --- | --- |
| `agents` | read, switch | The runs, filtered by state, agent and "only mine". |
| `agents/new` | write, switch, not archived | The task form: prompt, agent name, target branch, AI key and model, limits (changes, tokens, cost cap, paths, time), optional narrower write scope. Shows what the repository policy lets agents do. |
| `agents/runs/{id}` | read, switch | State, live activity, budget meters, access panel, and the result: the stack of changes, each linked to its review page. Stop and steer for people with write access. |
| `agents/runs/{id}/events?after=N` | read, switch | JSON poll that holds the request open until there is news. The page script follows a run with it. |
| `agents/tokens` | admin, switch | Agent tokens: make (name, agent, write globs, changes per run, lifetime of 1 hour to 90 days, budget), list with use and expiry, set budget, revoke. The secret is shown once. |
| `workflows` | read | The definitions in `.asc/workflows/` on trunk, whether each is on, the latest runs. |
| `workflows/{name}` | read (trigger and switch: write, switch) | Triggers, inputs, steps, the file as text, "Run workflow" form, on/off. |
| `workflows/runs` | read | Run history, filtered by workflow, state and change. Each row names the definition version it ran. |
| `workflows/runs/{id}` | read (act: write, switch) | Steps with state, per-step logs of agent steps, approvals, cancel, run again. |

`/-/asc/agents` also lists the person's recent runs across the repositories they work in and links to each repository's agent pages.

## Who is checked, and how asc is called

1. The hub checks Forgejo's own permission first: signed in, code read access, then write or admin as the action needs. A refusal that could tell a stranger that something exists is a 404.
2. Reads (lists, run pages, the feed, the policy) call asc with the internal token, after those checks.
3. Every call that changes something (start, stop, steer, make or revoke a token, set a budget, trigger, switch, decide an approval, run again) calls asc as the signed-in person. A browser session holds no asc credential, so the hub makes a one-use access token of that person for the call: scope `write:repository`, limited to this one repository, deleted when the call returns (a leftover of a crash is swept by that person's next call). asc then authorises and audits the action as that person: run owner or maintainer for stop, the named approver for approvals, repository admin for tokens, owner admin for AI keys. The hub never stands in for a person with the internal token.
4. Mutating forms carry an op id, so a double submit is one action.

## Untrusted text and secrets

- Everything asc sends about a run (titles, model output, tool output, errors, prompts) is untrusted. The hub drops invalid UTF-8, control characters, bidirectional overrides and zero-width characters, cuts the length, and the templates escape the rest. The live page script assigns text with `textContent` only; links come from the hub's own `changeUrl` and are used only when they are paths on this site.
- A run, token, workflow or agent run named in a URL is accepted only when asc's answer says it belongs to this repository and has the id that was asked for. Anything else is a bad answer and none of it is shown. Event streams must carry the run's id and rising sequence numbers.
- The text of an asc error is never shown or logged. Only its gRPC code and its `ErrorDetail` code are used, mapped to fixed sentences (`services/ascagentui/failure.go`).
- Agent token secrets are shown once, in the answer to the POST that made the token, with `Cache-Control: no-store`. They are not put in a redirect, a flash message, a log line or the list (the list shows the last four characters asc sends as `secret_hint`). A secret that does not have the shape `ascat_` plus 43 URL-safe characters is not shown.
- AI keys are never fetched. The picker gets label, provider, default model and state from `AiProviders.ListKeys` and the model ids from `ListModels`; only a key in state ACTIVE can be picked.
- Agents never push trunk. The run page says so, the policy panel repeats it, and the result is a stack of changes under the normal review and merge-queue path.
- A run's changes are drafts that only asc shows. "Open for review" on the run page (write access, a repository that takes pull requests) opens a pull request for each change that has none, bottom to top, as the agent's own bot account (`agent-<name>`, a user of type bot that cannot sign in; a person who already owns that name is never used). Each change is published for a moment under `asc/agent/<agent>/run-<n>-<change>` so the git view has its commit, the pull request is an AGit pull request of the change in the stack of the run, the branch is removed again, and the pull request remembers the run (`pull_request_asc_run`): its page links back to the run page. Opening a run twice opens nothing twice; a change that has no git id yet or has landed is reported and left out. Code: `services/ascagentui/runpulls.go`, `routers/web/repo/asc_agents_pulls.go`.

- A run that succeeded with changes opens itself. asc serve queues the outbox effect `agent_run_finished` when the run ends (`crates/asc-serve/src/agents/runtime.rs`, only for a run with changes) and delivers it, at least once, to `POST /api/internal/asc/agent-run-finished` with the internal token and the body `{repo, run, state, changes}` (`routers/private/asc_run_finished.go`). The hub reads the run from asc again and does what the button does (`services/ascagentui/runfinished.go`), but only when the run succeeded, the person who started it can still write to the repository, and `[asc] AUTO_OPEN_RUN_PULLS` is not false (default true). A change that is not in the git view yet answers 503, so asc delivers the event again; one hour after the run ended the hub stops asking and the button remains. A failed or cancelled run is never opened by itself.

## Budgets

The task form and the token form send limits, never the hub's own defaults: an empty field is 0, which asc reads as the policy default. Numbers are digits only with upper bounds; cost is dollars with up to six decimals, converted to micro-USD without floating point. The meters show use against the limit with the words ("116k of 200k") and a native `<progress>` element. asc enforces the budgets on every update and every provider call.

## Agent activity in the repository lists

The repository lists (Explore, an organization, a profile, Watching) have quick chips above the list: All, Has open
stacks, Agent activity, Starred, Templates and Archived.

- **Agent activity** keeps the repositories where an agent acted in the last 7 days. An agent acts on the hub as its
  bot account (`agent-<name>`, a user of type bot that cannot sign in), so this is any action of a bot account in the
  activity log.
- **Has open stacks** keeps the repositories with an open pull request.
- **Starred** keeps what the viewer starred. It shows only to people who are signed in, on a hub with stars.

A chip adds to the search text, the sort and the language. A search and the pager keep it. With no result, the empty
state names the chip with a link that removes it. Forks and Mirrors stay in the Sort and filter sheet.

## Agents in the people directory

The people directory (Explore, People) lists agents with people. An agent row carries an Agent chip. The filter bar
has:

- **Show**: People and agents (the default), People, or Agents.
- **Organization**: the members of one organization. The field suggests the organizations you may see, and takes any
  name. You see the public members, or every member if you are a member of the organization or an admin. An
  organization you may not see finds no one.
- **Sort, Most active**: most actions in the last 90 days first. Only actions anyone may see count: those in public
  repositories of public owners, by people who do not keep their activity private. A tie goes to the name.

The pager keeps the filters. With no result, the list says so and offers to clear the filters.

A signed-in person also sees the role of each account and a **Roles on this hub** card with the count of each:

| Role | Who |
| --- | --- |
| Owner | A site administrator. |
| Maintainer | Can write to a public repository of a public owner, as a collaborator or a team member with write. Write access to a private repository is not shown, so the role does not tell that a private repository exists. |
| Member | Anyone else with an account. |
| Agent | A bot account an agent acts as. |

Roles tell who runs the hub, so a visitor or a restricted account sees the Agent chip only. The counts include only
the active accounts the viewer may see.

## Activity in the people and organization directories

Each row of the people and organization directories shows a line of the public actions of the last 12 weeks, one point
per week, oldest on the left. The line has the same fact as text for a screen reader ("5 public actions in the last 12
weeks"); a dashed line means no public activity. Only what anyone may see is counted: actions in public repositories
of owners with a public profile, each action once. Agents act as their bot accounts, so their work is in the line.

- A person also shows the changesets (pull requests) they opened and the reviews they gave (approve, comment, request
  changes) in the window. A person who keeps their activity private shows none of this, except to themself and a site
  administrator.
- An organization also shows when it was last active ("Active 12 minutes ago"). A private or limited organization has
  no public activity, so its row shows an empty line.

## Who contributes, on each row

Each row of the repository lists shows a stack of up to four avatars: the people and agents that acted in the
repository in the last 90 days, most active first. The stack is labelled with the total ("3 contributors in the last
90 days"). An agent has a violet ring and its name reads "name (agent)". Each action counts once, even when the
activity log copies it to the feed of every watcher. A person who keeps their activity private, or whose profile the
viewer may not see (a private profile, seen by a visitor), is left out of the stack and the total; an admin and the
person themselves still see them. A repository without activity in the window has no stack.

The chips are read by `routers/web/shared/repoquick` and filtered by `AscQuickFilter` in
`models/repo/asc_quick_filter.go`. Tests: TestTheQuickChipsOfTheRepositoryLists, TestTheQuickChipsOfExplore.

## Files

- `routers/web/repo/asc_agents*.go`, `asc_workflows.go`: handlers. `routers/web/user/asc_agent_tokens.go`: "Your agents".
- `routers/web/asc_agents_routes.go`: the route table (registered by two lines in `web.go`).
- `services/ascagentui`: validators, budget arithmetic, state labels, the failure classifier, the switch, the one-use token, the change to pull request links.
- `templates/repo/asc_agents*.tmpl`, `asc_workflows*.tmpl`, `templates/user/asc_agents.tmpl`.
- `web_src/css/asc-agents.css` (theme variables, 44px targets, mobile first), `web_src/js/features/asc-agent-run.ts` (live run and workflow pages, model picker).

## Tests

`go test -tags 'sqlite sqlite_unlock_notify' ./services/ascagentui ./routers/web/repo ./routers/web/user -run 'Asc(Agents|Workflow)|UserCall|Features|ChangeLinks|Classify|Parse|Meters|Clean|Check|Validators|Formatters|Run|Change|Times'`.

The handler tests start a stand-in `asc serve` (`routers/web/repo/asc_agents_fake_test.go`) with the five services and check, for every route: signed out, switch off, reader, admin, archived; the request asc received (fields, op id, which credential); that nothing reaches asc when the hub refuses; that planted secrets and asc error text never reach a page; that bad answers (another repository's run, wrong id, falling sequence numbers, a malformed secret) are not shown.

## Not here

There is no plan view, no checkpoint rollback and no audit download on the run page: asc has no calls for them yet. The audit events of agent and workflow actions (`agent.run.created`, `workflow.triggered`, ...) are written by asc and appear on the audit page when that exists.
