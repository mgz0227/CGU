# CGU v1.5.8

## Admissions and student accounts

- Require an English name for new admissions and generate registrar IDs as `CGU-<ENGLISH>-<YEAR>-<COLLEGE>-<DEPARTMENT>-<CLASS>-<SEAT>`.
- Approving an application creates the student record, university mailbox, and administrator notification automatically.
- Keep initial passwords out of API responses, browser state, internal mailbox copies, and database projections. Deliver them only through the configured SMTP relay.
- Add a dedicated credential resend action that rotates the password, revokes existing student sessions, and queues a new SMTP message without exposing the password to administrators.
- Allow administrators to correct an approved applicant's contact email and synchronize pending credential delivery safely.

## Registrar operations

- Add working edit and delete controls for admissions applications, including deletion of pending and terminal applications.
- Add permanent student-account deletion with transactional cleanup of linked admissions, enrollments, grades, schedules, mailboxes, and notifications.
- Reject ambiguous or cross-field legacy identifiers instead of deleting another account's records.
- Keep approval idempotent and fail closed for malformed or conflicting legacy data.

## Web and security

- Ship synchronized `v1.5.8` static assets and i18n resources so cached old bundles cannot hide current controls.
- Preserve browser-language detection, Chinese/English content management, CSRF/origin checks, secure cookies, rate limits, and SMTP settings in the administrator console.
- Generic mailbox retry cannot be used to resend onboarding credentials; use the dedicated credential action.
- Remove historical demo-account seed data; only the configured bootstrap administrator is retained.

## Verification

- `go test ./... -count=1`
- `go vet ./...`
- `go build -trimpath -ldflags '-s -w'`
- `git diff --check`
