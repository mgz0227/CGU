# CGU v1.5.4

## Complete admissions operations

- The administrator admissions screen exposes one workflow decision: **Approve admission**.
- Approval remains an idempotent transaction that provisions the student record, university mailbox, durable notification, and one-time initial credentials.
- Applicant contact details and internal review notes can be edited without changing the decision; approved student IDs are protected from accidental rewrites.
- Accepted records missing their linked account can be repaired through the same approval action; rejected and withdrawn history stays read-only.
- SMTP onboarding delivery runs independently from the approval response when a real relay is configured, while the internal mailbox record remains durable and retryable.

## Academic portal reliability

- Students can rotate their own password from the profile page after verifying the current password; other sessions are revoked immediately.
- Course department values, bilingual announcement bodies, and published/draft state remain editable in the administration console.
- Optional course and announcement fields, plus admission notes, support explicit clearing instead of silently restoring the previous value.
- Administrators can disable or re-enable student accounts; disabling revokes active sessions and blocks future sign-in while preserving academic history.
- The public site now provides a live campus calendar, a dynamic CSV course catalogue download, and detailed bilingual programme dialogs whose apply action opens the application form with the selected school prefilled.
- Public content overrides can be intentionally cleared in both languages to restore the bundled translation without leaving stale text in the current browser.
- Root-relative assets keep `/login/`, `/portal/`, `/admin/`, and `/calendar/` fully styled when a reverse proxy preserves a trailing slash.
- Mobile portal navigation now grows with the viewport and scrolls instead of clipping the final academic and content-management entries.
- Admin and student data refreshes use bounded, partial-failure handling so one unavailable endpoint cannot leave every table stuck on a loading state.
- Public application submission and administrative editors prevent duplicate in-flight submissions and show localized, non-diagnostic errors.
- Admin refresh responses are version-checked so a slow request cannot overwrite a just-completed approval or editor save.

## Security and verification

- No demo accounts or passwords are included; bootstrap administrator credentials remain private configuration values.
- Passwords are stored only as bcrypt hashes, and initial credentials are returned only once during approval.
- Existing CSRF, origin, rate-limit, request-size, secure-cookie, SMTP validation, and durable delivery-lease protections remain enabled.
- MySQL-enabled production starts fail closed by default when the database is unavailable; memory fallback requires explicit opt-in through configuration.
- The internal mailbox is always available; optional SMTP is outbound delivery only and does not claim POP3/IMAP inbound synchronization.

## Verification

- `go test -count=1 ./...`
- `go vet ./...`
- `go build -o dist/cgu-v1.5.4-check.exe .`
- JavaScript syntax checks for `web/portal.js`, `web/i18n.js`, `web/script.js`, and `web/calendar.js`
- Browser checks for Chinese/English login rendering, admissions approval, automatic student/mailbox provisioning, content reset, bilingual draft editing, notes persistence, password rotation, and partial portal loading
