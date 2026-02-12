# OP — The Organic Programming CLI

> *"One command, every holon."*

OP is the unified entry point to the Organic Programming ecosystem.
It discovers holons — locally or over the network — and dispatches
commands to them through a single interface.

## Usage

```
# Promoted verbs (delegate to Sophia Who?)
op new                               → create a new holon identity
op list                              → list all known holons
op show <uuid>                       → display a holon's identity

# Full namespace (dispatch to any holon)
op who new                           → same as op new
op atlas pull                        → rhizome-atlas
op translate file.md --to fr         → babel-fish-translator

# Remote invocation
op @remote:8080 whisper transcribe   → gRPC call

# OP's own commands
op discover                          → list all available holons
```

## Status

Identity defined. Code not yet implemented.

## Organic Programming

This holon is part of the [Organic Programming](https://github.com/Organic-Programming/seed)
ecosystem. For context, see:

- [Constitution](https://github.com/Organic-Programming/seed/blob/master/AGENT.md) — what a holon is
- [Methodology](https://github.com/Organic-Programming/seed/blob/master/METHODOLOGY.md) — how to develop with holons
- [Terminology](https://github.com/Organic-Programming/seed/blob/master/TERMINOLOGY.md) — glossary of all terms
- [Contributing](https://github.com/Organic-Programming/seed/blob/master/CONTRIBUTING.md) — governance and standards
