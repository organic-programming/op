---
# Holon Identity v1
uuid: "28f22ab5-c62d-41f8-9ada-e34333060ff9"
given_name: "OP"
family_name: "CLI"
motto: "One command, every holon."
composer: "B. ALTER"
clade: "deterministic/stateful"
status: draft
born: "2026-02-12"

# Lineage
parents: ["b00932e5-49d4-4724-ab4b-e2fc9e22e108"]
reproduction: "manual"

# Pinning
binary_path: null
binary_version: "0.1.0"
git_tag: null
git_commit: null
os: "darwin"
arch: "arm64"
dependencies:
  - "b00932e5-49d4-4724-ab4b-e2fc9e22e108"  # sophia-who
  - "607239ea-41fd-4955-a828-9478ce866637"  # rhizome-atlas

# Optional
aliases: ["op"]
wrapped_license: null

# Metadata
generated_by: "sophia-who"
lang: "go"
proto_status: draft
---

# OP — The Organic Programming CLI

> *"One command, every holon."*

## Description

OP is the unified entry point to the Organic Programming ecosystem.
It discovers holons — locally or over the network — and dispatches
commands to them through a single interface. The actant installs one
binary and gets access to every holon.

OP does not implement domain logic. It routes.

## Dispatch model

```
op <holon> <command> [args...]       → local invocation (subprocess)
op @<host:port> <holon> <command>    → remote invocation (gRPC)
```

### Local discovery

OP discovers holons in order:

1. `holons/` directory (submodule siblings)
2. `$PATH` (installed holon binaries)
3. `~/.holon/cache/` (cached by Atlas)

### Remote discovery

OP connects to a gRPC endpoint and uses reflection or a known
service registry to discover available holons.

## Commands

```
op who list                          → dispatches to sophia-who
op atlas pull                        → dispatches to rhizome-atlas
op translate file.md --to fr         → dispatches to babel-fish-translator
op @remote:8080 whisper transcribe   → remote gRPC call

op discover                          → list all available holons (local + remote)
op status                            → health check on known endpoints
op version                           → show op version and holon registry
```

## Contract

- Proto file: `op.proto`
- Service: `OPService`
- RPCs: `Invoke`, `Discover`, `Status`

## Why OP is a holon

OP has a contract, a CLI, and tests — it follows its own constitution.
A holon that composes holons is still a holon. OP is the root of the
fractal — the first holon an actant encounters.
