# Write a workflow

A workflow is agent work that starts on an event and runs steps in order. It is a file on the trunk, at `.asc/workflows/<name>.toml`, so a new workflow or a change to one is reviewed like any other file. A run always uses the file as it is on the trunk when the run starts, never the copy inside an open change.

The quickest start is **Start from a template** on the Workflows page of a repository. It opens a new change with a working file that you can edit.

## A first file

```toml
description = "Review every new change"

[[trigger]]
kind = "change_opened"
branches = ["main"]

[[step]]
id = "review"
kind = "agent"
agent = "reviewer"
prompt = "Read change {{change}} and leave notes. Do not edit files."

[[step]]
id = "decide"
kind = "approval"
needs = ["review"]
approvers = ["role:maintainer"]
```

The file name without `.toml` is the name of the workflow. A file that is not valid is shown on the Workflows page with the reason, and it does not run.

## Triggers

A workflow has 1 to 16 `[[trigger]]` tables. Each one names a `kind`:

| Kind | Starts a run when |
| --- | --- |
| `manual` | someone selects **Run now** |
| `change_opened` | a change is opened |
| `change_updated` | a change gets a new version |
| `check_failed` | a check fails on a change |
| `comment_mention` | a comment mentions the agent named in `agent` |
| `land` | a change lands |
| `schedule` | the `cron` time comes (five fields, UTC) |

`branches` limits a trigger to target branches (`*` matches any run of characters). `paths` limits it to events that touch a matching path. Leave either out to match all.

## Inputs

An `[[input]]` has a `name`, an optional `description`, `required` and a `default`. **Run now** asks for the inputs. A prompt reads one as `{{input.<name>}}`.

## Steps

A workflow has 1 to 32 `[[step]]` tables. Each has an `id`, a `kind` and optional `needs` (the ids of the steps that must finish first). Steps without a link between them can run at the same time.

- **agent**: runs `agent` with `prompt`. `paths` limits what the agent may write. A prompt can use `{{change}}`, `{{input.<name>}}` and `{{event.<field>}}`.
- **command**: runs `command`, a list of arguments (never a shell line), in the sandbox on a working copy of the change. Values from inputs and events arrive only as the environment variables `ASC_INPUT_<NAME>`, `ASC_CHANGE` and `ASC_EVENT_<FIELD>`. `secrets` maps environment variables to repository secrets. `egress` lists the hosts that a command with secrets may reach. Without it, there is no network.
- **approval**: waits for one of the `approvers` (`user:<name>` or `role:<name>`).
- **checks**: waits for the named `checks` on the newest change of the run.

`timeout_seconds` sets the time limit of a step, up to 30 days. If a step fails, the run stops, unless that step sets `continue_on_failure = true`.

## Limits

`[budget]` limits the whole run: `max_changes`, `max_tokens`, `max_cost_micro_usd` (1,000,000 is one dollar) and `max_runtime_seconds`. The policy of the repository can set lower limits, and a lower limit always applies. `max_parallel_runs` (1 to 64) sets how many runs of this workflow can be active at the same time.

## Turn it on or off

Each workflow has a switch on the Workflows page. The switch is logged in the audit log. A workflow that is off does not start on its triggers, and **Run now** is not available for it.
