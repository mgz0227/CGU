# CGU v1.5.5

## University experience

- Add a public bilingual course catalog at `/catalog`, with keyword and term filters, a downloadable CSV, and a student handbook section linked to the student portal.
- Refresh the homepage and default registrar announcement from the official 23 August 2026 Snezhnaya news entries, while retaining the Version 7.0 polar studies curriculum.
- Add public section aliases for the university information architecture (`/about`, `/programs`, `/admissions`, `/campus-life`, `/news`, and `/contact`) while keeping one managed homepage source of truth.
- Add a student-scoped transcript download at `/api/transcript.csv`; only published grades are exported and administrator exports require an explicit student selector.
- Extend course records, admin forms, CSV exports, and student views with English school, instructor, description, and term metadata.
- Preserve course history: a course with enrollment, grade, or schedule records can no longer be deleted.

## Registrar integrity

- Honor the initial active/disabled state when an administrator creates a student account, and count only active students in dashboard statistics.
- Reject overlapping schedule entries for the same student while allowing adjacent classes.
- Enforce finite score/point ranges and keep unpublished grades out of student-facing transcripts.
- Keep admissions approval as the only decision transition; notes remain editable without changing the decision.

## Security and operations

- Force `Secure` on session cookies for TLS requests, add a bounded global bcrypt verification gate for login abuse, and retain login retry responses.
- Apply independent IP, account, and IP/account login buckets with a bounded identifier length to reduce credential-spray bypasses.
- Reject passwords above bcrypt's 72-byte limit before hashing, and neutralize spreadsheet formula prefixes in course and transcript CSV exports.
- Refuse static requests for configuration, environment, source, key, database, and other sensitive files even when `CGU_STATIC_DIR` is accidentally pointed at a repository or deployment directory.
- Harden portal hash navigation with an allowlist and expand regression coverage for static routes, transcript isolation, TLS cookies, and sensitive-file blocking.
- Keep MySQL course bilingual migrations idempotent while preserving intentional administrator clears after restart.
- Migrate the previous built-in Snezhnaya announcement and homepage dates only when their stored values still match the old shipped copy, preserving administrator edits.
- Browser assets remain same-origin, CSP-protected, and syntax-checked; no demo credentials or production secrets are included.

## Verification

- `go test -count=1 ./...`
- `go vet ./...`
- `go build -o dist/cgu-next.exe .`
- Browser smoke checks for `/catalog`, automatic English rendering, admin login/editor controls, public aliases, and CSV downloads.

The local Windows environment has no C compiler, so `go test -race ./...` is delegated to the GitHub Actions Ubuntu runner. SMTP delivery still requires private provider credentials, MySQL durability, HTTPS, and deployment-side SPF/DKIM/DMARC configuration.
