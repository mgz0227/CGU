# CGU v1.5.3

## Admissions automation

- Admissions now has one decision action: **Approve admission**.
- Approval atomically provisions the student record, generated university mailbox, durable admin notification, and one-time initial credentials.
- Repeated approval requests are idempotent and never return the initial password again.
- The applicant onboarding notice is queued through the internal mailbox and can be delivered through the configured SMTP relay without placing the password in message content.
- SMTP delivery state remains visible after an administrator reloads the admissions page, with retry support for safe failure states.

## Campus operations

- Student portal mailboxes, courses, grades, schedules, announcements, admissions, notifications, and bilingual site content remain available through the Go service and optional MySQL persistence.
- Public applications cannot inject workflow status or internal notes; administrator status changes must use the approval action.
- Browser language preferences automatically select Chinese or English, while administrators can edit managed copy and media links from the backend.
- API and frontend requests now fail with a bounded timeout instead of leaving tables or application buttons stuck in a loading state.

## Security and release quality

- Bootstrap authentication remains configuration-driven; no public demo accounts are seeded.
- Initial passwords are generated with crypto/rand, persisted only as bcrypt hashes, and excluded from database records, mailbox bodies, list projections, and replay responses.
- CSRF, origin, request-size, rate-limit, secure-cookie, SMTP address-validation, and durable delivery-lease protections remain enabled.
- Release notes use real Markdown line breaks and are verified against the GitHub release body after publication.

## Verification

- `go test -count=1 ./...`
- `go vet ./...`
- `go build ./...`
- JavaScript syntax checks for `web/portal.js`, `web/i18n.js`, and `web/script.js`
- End-to-end Chrome checks for application submission, approval, reload, replay, student login, mailbox access, admin content editing, and browser-language i18n
- MySQL-backed startup and health check against the configured test database
