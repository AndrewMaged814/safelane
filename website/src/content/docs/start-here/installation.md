---
title: Installation
description: Install SafeLane from Go or build it from source.
---

## A release gate is only useful if the binary is the one you can inspect

SafeLane is a Go CLI. Install it from the repository or build the source in your checkout.

### Go install

    go install github.com/AndrewMaged814/safelane/cmd/safelane@main

The binary goes to GOPATH/bin or GOBIN. Put that directory on PATH. SafeLane requires Go 1.26.5 or later.

### Build from source

    git clone https://github.com/AndrewMaged814/safelane.git
    cd safelane
    go build -o ./bin/safelane ./cmd/safelane
    ./bin/safelane version
    go test ./...

## Next

- [Setting Up an Application](../guides/setting-up/)
- [Installing the Agent Skill](../guides/agent-skill/)
- [CLI Command Reference](../reference/cli/)

