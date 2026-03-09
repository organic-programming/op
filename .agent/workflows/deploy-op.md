---
description: Build, version-bump, deploy, and push the op CLI
---

# Deploy op

Every time `op` is modified and deployed, the **minor version must increment**.
The version lives in `cmd/op/main.go` as `var version = "X.Y.Z"`.

## Steps

1. Increment the minor version in `cmd/op/main.go` (e.g., `0.1.0` → `0.2.0`).
   Reset patch to 0 unless the change is a hotfix (then increment patch only).

// turbo
2. Run tests:
   ```
   cd holons/grace-op && go test ./...
   ```

3. Build and deploy with commit hash:
   ```
   cd holons/grace-op && \
     git add -A && \
     git commit -m "<commit message>" && \
     COMMIT=$(git rev-parse HEAD) && \
     go build -ldflags "-X main.commit=$COMMIT" -o /usr/local/bin/op ./cmd/op && \
     git push
   ```

4. Verify:
   ```
   op version
   ```
   Expected output: `op X.Y.Z (<commit>)`

## Rules

- The commit hash is injected via `-ldflags "-X main.commit=<hash>"`.
- Always deploy to `/usr/local/bin/op`.
- Always push after deploying.
- Always update parent submodule pointers (organic-programming → videosteno).
