# OP — The Organic Programming CLI

> *"One command, every holon."*

OP is the unified entry point to the Organic Programming ecosystem.
It discovers holons — locally or over the network — and dispatches
commands to them through a single interface.

## Install

```bash
go install github.com/organic-programming/grace-op/cmd/op@latest
```

The binary is installed as `op` (not `grace-op`) because the Go module
entry point is `cmd/op`. Make sure `$(go env GOPATH)/bin` is in your
`PATH`.

### From Source

```bash
git clone https://github.com/organic-programming/grace-op.git
cd grace-op
go build -o op ./cmd/op
sudo mv op /usr/local/bin/   # or anywhere in your PATH
```

## Usage

```
# Promoted verbs (delegated to the `sophia-who` holon over mem://)
op new                               → create a new holon identity
op list                              → list all known holons
op show <uuid>                       → display a holon's identity
op pin <uuid> --version 1.0.0        → capture version/commit/arch

# Full namespace (dispatch to any holon binary)
op sophia-who list                   → direct holon dispatch
op who list                          → alias of sophia-who
op atlas pull                        → marco-atlas
op translate file.md --to fr         → abel-fishel-translator

# OP's own commands
op discover                          → list all available holons
op version                           → show op version
```

## Sophia Who? list over every transport

Use `ListIdentities` (the gRPC equivalent of `sophia-who list`) through each
transport supported by Sophia Who?:

```bash
# 1) CLI facet (delegated command)
op who list .
op sophia-who list .

# 2) Promoted verb (same provider behavior as `sophia-who list`)
op list .

# 3) gRPC over TCP (persistent server)
op run sophia-who:9090
op grpc://localhost:9090 ListIdentities '{}'
# stop with: kill <pid printed by op run>

# 4) gRPC over Unix socket (persistent server)
op run sophia-who --listen unix:///tmp/who.sock
op grpc+unix:///tmp/who.sock ListIdentities '{}'
# stop with: kill <pid printed by op run>

# 5) gRPC over stdio (ephemeral, no `op run`)
op grpc+stdio://sophia-who ListIdentities '{}'
```

## Status

v0.1.0 — promoted verbs, discover, namespace dispatch.

## Design Drafts

- [OP_BUILD_SPEC.md](OP_BUILD_SPEC.md) — proposed contract for
  manifest-driven `op build`, including composite holons and `recipe`
  orchestration.

## Organic Programming

This holon is part of the [Organic Programming](https://github.com/organic-programming/seed)
ecosystem. For context, see:

- [Constitution](https://github.com/organic-programming/seed/blob/master/AGENT.md) — what a holon is
