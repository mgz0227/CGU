# CGU v1.5.9

## Administration interface

- Tighten admission-row heading spacing so pending and approved applications remain compact while edit, delete, and credential-resend actions stay visible.
- Keep the synchronized `v1.5.7` and `v1.5.8` compatibility assets aligned with the current portal stylesheet.

## Verification

- `go test ./... -count=1`
- `go vet ./...`
- `go build -trimpath -ldflags '-s -w'`
- `git diff --check`
