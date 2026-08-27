# CGU v1.5.10

## Cache-busted web assets

- Move every HTML entry point to the new `/assets/v1.5.10/` bundle so edge caches cannot serve the pre-fix `v1.5.8` stylesheet.
- Publish the compact admissions-row heading rule in the new bundle while retaining `v1.5.7` and `v1.5.8` compatibility assets.

## Verification

- `go test ./... -count=1`
- `go vet ./...`
- `go build -trimpath -ldflags '-s -w'`
- `git diff --check`
