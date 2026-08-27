# CGU v1.5.11

## Static asset consistency

- Version the shared stylesheet references on the calendar and catalog pages so every public entry point bypasses stale edge-cache keys.
- Require all nine `v1.5.11` browser assets to be reachable with the revalidation cache policy in the static-route regression test.

## Verification

- `go test ./... -count=1`
- `go vet ./...`
- `go build -trimpath -ldflags '-s -w'`
- `git diff --check`
