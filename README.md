---
# Cartouche v1
title: "OP — The Organic Programming CLI"
author:
  name: "B. ALTER"
  copyright: "© 2026 Benoit Pereira da Silva"
created: 2026-02-12
revised: 2026-02-12
lang: en-US
origin_lang: en-US
translation_of: null
translator: null
access:
  humans: true
  agents: false
status: draft
---
# OP — The Organic Programming CLI

> *"One command, every holon."*

OP is the unified entry point to the Organic Programming ecosystem.
It discovers holons — locally or over the network — and dispatches
commands to them through a single interface.

## Usage

```
# Promoted verbs (delegated to Sophia Who? binary)
op new                               → create a new holon identity
op list                              → list all known holons
op show <uuid>                       → display a holon's identity
op pin <uuid> --version 1.0.0        → capture version/commit/arch

# Full namespace (dispatch to any holon binary)
op who list                          → same as op list
op atlas pull                        → rhizome-atlas
op translate file.md --to fr         → abel-fishel-translator

# OP's own commands
op discover                          → list all available holons
op version                           → show op version
```

## Sophia Who? list over every transport

Use `ListIdentities` (the gRPC equivalent of `who list`) through each
transport supported by Sophia Who?:

```bash
# 1) CLI facet (delegated command)
op who list .

# 2) Promoted verb (same provider behavior as `who list`)
op list .

# 3) gRPC over TCP (persistent server)
op run who:9090
op grpc://localhost:9090 ListIdentities '{}'
# stop with: kill <pid printed by op run>

# 4) gRPC over Unix socket (persistent server)
op run who --listen unix:///tmp/who.sock
op grpc+unix:///tmp/who.sock ListIdentities '{}'
# stop with: kill <pid printed by op run>

# 5) gRPC over stdio (ephemeral, no `op run`)
op grpc+stdio://who ListIdentities '{}'
```

## Status

v0.1.0 — promoted verbs, discover, namespace dispatch.

## Organic Programming

This holon is part of the [Organic Programming](https://github.com/organic-programming/seed)
ecosystem. For context, see:

- [Constitution](https://github.com/organic-programming/seed/blob/master/AGENT.md) — what a holon is
- [Methodology](https://github.com/organic-programming/seed/blob/master/METHODOLOGY.md) — how to develop with holons
- [Terminology](https://github.com/organic-programming/seed/blob/master/TERMINOLOGY.md) — glossary of all terms
- [Contributing](https://github.com/organic-programming/seed/blob/master/CONTRIBUTING.md) — governance and standards
