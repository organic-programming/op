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

## Facet dispatch — three ways to reach a holon

A holon exposes multiple facets: CLI, gRPC, and API. OP dispatches to
the right facet based on the addressing scheme:

### 1. CLI facet — `op <holon> <command>`

Finds the holon binary and runs it as a subprocess. Fast, simple, local.

```
op who list                          → subprocess: who list
op atlas pull                        → subprocess: atlas pull
op translate file.md --to fr         → subprocess: translate file.md --to fr
```

### 2. gRPC facet — `op grpc://<address> <method>`

Connects to a holon's gRPC server and calls an RPC using **server
reflection**. This works with ANY holon in ANY language — Go, Swift,
Rust, Python — as long as it exposes a gRPC server with reflection
enabled.

**Ephemeral mode** — OP starts the binary, calls, and stops:

```
op grpc://who ListIdentities
```

What happens:
1. OP finds the `who` binary
2. Allocates an ephemeral port
3. Launches `who serve --port <port>`
4. Waits for the TCP port to be ready
5. Connects via gRPC reflection (no compiled stubs)
6. Calls `ListIdentities` with a dynamic protobuf message
7. Prints the JSON response
8. Kills the server process

**Existing server** — connect to a running server:

```
op grpc://localhost:9090 ListIdentities
op grpc://192.168.1.10:9090 CreateIdentity '{"givenName":"X","familyName":"Y","motto":"Z","composer":"A"}'
```

**List available methods** — omit the method name:

```
op grpc://localhost:9090
```

### 3. API facet — Go import (in-process)

OP uses Sophia's `pkg/identity` as a direct Go import for the promoted
verbs. No subprocess, no gRPC, no overhead. This is possible because
both OP and Sophia are written in Go.

```go
import "github.com/Organic-Programming/sophia-who/pkg/identity"

holons, _ := identity.FindAll(".")
```

## The `run` command — language-agnostic server launcher

`op run` starts any holon's gRPC server as a background process.
The holon can be written in any language — all it needs is a `serve`
command that accepts `--port`.

```
op run who:9090
# op run: started who (pid 12345) on port 9090
# op run: connect with: op grpc://localhost:9090 <method>
# op run: stop with:    kill 12345
```

Then connect:

```
op grpc://localhost:9090 ListIdentities
op grpc://localhost:9090 CreateIdentity '{"givenName":"Test"}'
```

**Cross-language example** — a holon written in Swift:

```
# The Swift holon binary understands: myholon serve --port <port>
op run myholon:9090
op grpc://localhost:9090 ProcessImage '{"path":"/tmp/photo.jpg"}'
kill $(pgrep myholon)
```

OP never needs to know what language the holon is written in. The
contract (`.proto`) is the universal bridge. gRPC reflection is the
universal discovery mechanism.

## Promoted verbs

Some holon commands are so fundamental they become top-level verbs.
These delegate to Sophia Who? via the API facet (no subprocess):

```
op new                               → creates a new holon identity
op list                              → lists all known holons
op show <uuid>                       → displays a holon's identity
op pin <uuid>                        → captures version/commit/arch
```

## Local discovery

OP discovers holons in order:

1. `holons/` directory (submodule siblings)
2. `$PATH` (installed holon binaries)
3. `~/.holon/cache/` (cached by Atlas)

```
op discover                          → list all available holons
```

## Commands summary

```
# Promoted verbs (API facet → Sophia)
op new / list / show / pin

# CLI facet (subprocess)
op <holon> <command> [args]

# gRPC facet (network)
op grpc://<holon> <method>           ephemeral server
op grpc://<host:port> <method>       existing server
op run <holon>:<port>                start a holon's server

# OP's own
op discover                          list available holons
op serve [--port 9090]               start OP's own gRPC server
op version                           show op version
op help                              this message
```

## Contract

- Proto file: `api/op.proto`
- Service: `OPService`
- RPCs: `Discover`, `Invoke`, `CreateIdentity`, `ListIdentities`,
  `ShowIdentity`, `PinVersion`

## Why OP is a holon

OP has a contract, a CLI, a gRPC server, and tests — it follows its
own constitution. A holon that composes holons is still a holon. OP is
the root of the fractal — the first holon an actant encounters.
