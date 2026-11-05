# Install ASC

ASC is one static binary. It installs without administrator rights, verifies its own signature, and runs on macOS, Linux and Windows.

## Before you start

You need an ASC Hub account, or any server that speaks the ASC protocol. You do not need other version control tools installed.

The hub shows the install, verify and connect steps of its current release on this page, with the commands for your system. When the hub has no release to offer, it shows how to build `asc` from source instead.

<!-- asc-install-steps -->

## Update and sign out

Run `asc upgrade` to move to the latest release, or let your package manager do it. `asc upgrade --check` only says whether a newer version exists. An upgrade verifies the minisign signature before it unpacks anything, and keeps the old version beside the new one.

`asc auth status` shows the hubs you are signed in to. `asc auth logout --host <hub>` removes the token of one hub; your repositories are untouched.

## Troubleshooting

- **The signature does not verify.** Do not run the file. Download it again from the download page of your hub, and check that the public key matches the one shown there.
- **`asc auth login` cannot open a browser.** Copy the address it prints into a browser on any device, and approve the device there.
- **A container or CI job has no browser.** Create an access token in your settings and set `ASC_TOKEN` in the job's environment instead of signing in.
