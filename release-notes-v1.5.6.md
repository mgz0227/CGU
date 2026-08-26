# CGU v1.5.6

## Administrator-managed SMTP

- Move SMTP relay settings out of `config.json`, `.env`, and `CGU_SMTP_*` process variables into the protected administrator portal at `/admin#admin-smtp`.
- Add bilingual desktop and mobile controls for relay host, port, authentication, TLS mode, sender identity, HELO name, timeout, and an explicit test-message action.
- Persist settings in the new MySQL `cgu_smtp_settings` table and activate a saved relay immediately without restarting the Go service.
- Keep a blank password unchanged on later edits; API reads return only `passwordConfigured` and never expose the secret or ciphertext.

## Security and reliability

- Encrypt the SMTP password with authenticated AES-GCM before database storage. Deployments can provide a dedicated `CGU_SETTINGS_ENCRYPTION_KEY`; the bootstrap administrator secret is the compatibility fallback.
- Require an administrator session, same-origin checks, and the existing `X-CGU-Request` CSRF header for all SMTP changes and test sends.
- Validate every setting server-side, reject CRLF/control-character injection, enforce secure TLS defaults, and bound simultaneous SMTP test sends.
- Refuse SMTP setting writes without MySQL durability instead of silently retaining credentials in process memory.
- Remove the SMTP object and all `CGU_SMTP_*` examples from repository configuration templates.
- Add an administrator-only delete action for admissions that have not provisioned
  a student account. The admission notification is removed in the same database
  transaction; approved applications remain protected and return a conflict.

## Verification

- `go test ./...`
- `go vet ./...`
- `go build -o dist/cgu-smtp-admin.exe .`
- Authenticated API smoke checks for login, redacted SMTP reads, and explicit MySQL-required errors in memory mode.
- Chrome smoke checks for Chinese/English rendering, the password field, SMTP navigation, and a 390 px mobile viewport without horizontal overflow.

The Windows workstation does not have a C compiler, so `go test -race ./...` remains enforced by the GitHub Actions Ubuntu runner together with `govulncheck`.
