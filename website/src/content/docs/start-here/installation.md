---
title: Installation
description: Install a checksummed SafeLane release on Windows, macOS, or Linux.
---

## A release gate is only useful if the binary is the one you can inspect

SafeLane's installers download a precompiled binary from the latest GitHub Release and
verify its SHA-256 checksum before replacing an existing installation. Go is not required.

### macOS and Linux

    curl -fsSL https://raw.githubusercontent.com/AndrewMaged814/SafeLane/main/docs/install.sh | sh

The binary is installed to `~/.local/bin/safelane`. If that directory is not already on
`PATH`, the installer prints the exact follow-up required.

### Windows PowerShell

    irm https://raw.githubusercontent.com/AndrewMaged814/SafeLane/main/docs/install.ps1 | iex

The binary is installed to `%LOCALAPPDATA%\SafeLane\bin\safelane.exe`. The installer
adds that directory to the beginning of the user `PATH`; restart the terminal after the
first installation.

Rerun the same installer command to upgrade. Both installers reuse the same canonical
path, so upgrades do not leave a second active copy behind.

To inspect a script before running it, open
[`install.sh`](https://github.com/AndrewMaged814/SafeLane/blob/main/docs/install.sh) or
[`install.ps1`](https://github.com/AndrewMaged814/SafeLane/blob/main/docs/install.ps1).

### Build from source

Building requires Go 1.26.5 or later.

    git clone https://github.com/AndrewMaged814/safelane.git
    cd safelane
    go build -o ./bin/safelane ./cmd/safelane
    ./bin/safelane version
    go test ./...

## Next

- [Setting Up an Application](../guides/setting-up/)
- [Installing the Agent Skill](../guides/agent-skill/)
- [CLI Command Reference](../reference/cli/)

