# CGU v1.5.7

## Admissions operations

- Add a complete administrator editor for applicant name, contact email, applied school, and review notes.
- Allow legacy accepted applications to be repaired or removed while no student account has been provisioned.
- Keep provisioned student identity fields immutable; notes remain available for registrar follow-up.
- Preserve administrator CSRF checks, validation, atomic persistence, and the existing protection for provisioned records.

## Access and delivery

- Redirect the configured `www` host to the canonical public origin before rendering static pages, preventing origin-policy login failures.
- Publish versioned portal assets under `/assets/v1.5.7/` so a stale CDN copy cannot pair old JavaScript with new HTML.

## Verification

- `go test ./...`
- `go vet ./...`
- `go build -o dist/cgu-v1.5.7.exe .`
- Browser checks for canonical-host login, admissions editing, deletion, and Chinese/English rendering.
