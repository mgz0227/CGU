# CGU security audit

Audit date: 2026-08-24

Scope: Go HTTP service, cookie authentication, MySQL adapter, and browser
client code in `web/`. The audit follows the repository's Go and web
security checklist. No production secret is stored in this report.

## Executive summary

The service now has bounded HTTP parsing, same-origin plus custom-header CSRF
defence, secure response headers, adaptive bcrypt password hashes, legacy hash
migration, login failure throttling, bounded in-memory sessions, parameterized
SQL, and no persisted bearer token in the browser. `go test ./...`, `go vet
./...`, and a production build are required gates before release.

This is a hardening audit, not a guarantee that an internet-facing deployment
is safe without TLS, a reverse proxy, monitoring, and secret rotation.

## Findings and dispositions

### GO-HTTP-001: unbounded request headers (High, fixed)

- Location: `main.go`, `http.Server` construction.
- Evidence: the server now sets `MaxHeaderBytes` to 1 MiB and has read-header,
  read, write, and idle timeouts.
- Impact: limits header-memory exhaustion and slow-client resource retention.
- Fix: explicit server limits in the production entry point.
- Residual mitigation: keep a proxy-level request/header limit as a second
  boundary.
- False-positive note: tests using `httptest.NewServer` exercise the handler;
  the production `http.Server` limits are checked in the build review.

### GO-AUTH-001: password hashing (High, fixed with migration path)

- Location: `main.go` password helpers and `Store.authenticate`.
- Evidence: new hashes use bcrypt with the library default adaptive cost;
  successful legacy `pbkdf2-sha256` logins are rehashed and persisted.
- Impact: slows offline guessing and avoids retaining old hashes indefinitely.
- Fix: bcrypt format is prefixed with `bcrypt$`; old PBKDF2 is accepted only
  to complete migration.
- Residual mitigation: require all seeded/deployed passwords to be replaced,
  then remove PBKDF2 compatibility in a future breaking migration.
- False-positive note: bcrypt is intentionally CPU-expensive; login throttling
  below limits online abuse.

### GO-AUTH-002: login abuse and session exhaustion (High, fixed)

- Location: login rate limiter and session creation in `main.go`.
- Evidence: failures are limited to eight attempts per five-minute window per
  client/identifier, followed by a fifteen-minute block; both limiter keys and
  sessions are bounded and expired entries are pruned.
- Impact: reduces credential stuffing and memory exhaustion against the admin
  endpoint.
- Fix: `429` plus `Retry-After` is returned while blocked; successful login
  clears the failure record.
- Residual mitigation: put an IP/WAF limit at the edge and use a shared store
  for sessions/rate limits when running multiple replicas.

### GO-AUTH-003: account enumeration timing (Medium, fixed)

- Location: `Store.authenticate` in `main.go`.
- Evidence: unknown identifiers now run a fixed bcrypt comparison before the
  same generic `401` response used for a wrong password.
- Impact: reduces timing-based discovery of administrator usernames.
- Fix: constant dummy bcrypt hash and identical public error wording.
- Residual mitigation: keep login responses generic in the proxy and monitoring
  layer as well.

### GO-AUTH-004: bootstrap administrator credentials (Critical, fixed)

- Location: startup validation in `main.go` and seeded-user persistence in
  `database.go`.
- Evidence: startup refuses to serve without an explicitly configured
  administrator password; the configured hash and username are synchronized
  to the bootstrap administrator row in MySQL.
- Impact: prevents accidental internet exposure of a known administrator
  credential and makes environment-based password rotation effective.
- Fix: fail closed before opening the listener, persist the configured hash
  with a parameterized upsert, and remove the legacy seeded student account.
- Residual mitigation: use a unique secret-managed password and rotate it
  through the deployment secret store.

### GO-CSRF-001: state-changing browser requests (High, fixed)

- Location: `ServeHTTP`, `originAllowed`, and `web/portal.js`.
- Evidence: POST/PUT/PATCH/DELETE API requests require both an allowed Origin
  (when supplied) and `X-CGU-Request: 1`; CORS allows only the explicit
  same-origin contract and the cookie is `HttpOnly`, `SameSite=Lax`.
- Impact: blocks cross-site form/fetch attempts against authenticated admin
  actions and logout/login endpoints.
- Fix: frontend sends the header; preflight advertises only the required
  headers; forwarded scheme headers are not trusted.
- Residual mitigation: terminate TLS at a trusted proxy and preserve the
  public host correctly.

### GO-HTTP-002: browser security policy (Medium, fixed)

- Location: `setSecurityHeaders` in `main.go`.
- Evidence: CSP disables objects and frames, limits scripts to `'self'`,
  limits images to the known Unsplash host, and restricts browser capabilities.
- Impact: reduces XSS impact, clickjacking, and unwanted browser API access.
- Fix: CSP, `X-Frame-Options`, `Referrer-Policy`, `nosniff`, Permissions Policy,
  and HTTPS HSTS are emitted.
- Residual mitigation: keep third-party assets allow-listed and review any
  future inline script or external font addition before merging.

### JS-AUTH-001: token persistence (High, fixed)

- Location: `web/portal.js`.
- Evidence: the client no longer writes or reads access tokens from
  `localStorage`; authentication is the `HttpOnly` session cookie.
- Impact: a client-side injection cannot directly read a persisted bearer
  token.
- Fix: removed the token key and Authorization-header fallback; only the
  non-sensitive user display state remains in browser storage.
- Residual mitigation: keep dynamic HTML values escaped before any
  `innerHTML` assignment and do not add third-party scripts without review.

### GO-SQL-001: SQL injection (Informational, verified)

- Location: `database.go` queries and persistence functions.
- Evidence: application values are passed as database parameters; seed
  backfills use fixed SQL statements and `INSERT IGNORE` without interpolated
  user input.
- Impact: no observed string-built SQL injection path.
- Fix/mitigation: retain parameterized queries and least-privilege MySQL user.
- False-positive note: database credentials must never be committed or placed
  in URLs/logs.

## Deployment requirements

1. Serve behind HTTPS and set `CGU_COOKIE_SECURE=true` (or the equivalent
   config-file value). Never expose the development HTTP listener directly to
   the public internet.
2. Inject `CGU_ADMIN_USERNAME`, `CGU_ADMIN_PASSWORD`, and MySQL credentials
   from a secret manager or protected environment. Rotate the administrator
   secret through that same mechanism.
3. Restrict the MySQL account to the CGU schema, bind MySQL to a private
   network, and enable encrypted transport where supported.
4. For more than one Go replica, move sessions and login throttling to a
   shared store and configure an edge rate limit.
5. Run `go test ./...`, `go test -race ./...`, `go vet ./...`, and
   `govulncheck ./...` in CI. The 2026-08-24 scan found zero reachable
   vulnerabilities; the current `golang.org/x/crypto` module reports only its
   unmaintained, unused `openpgp` package. A missing `govulncheck` binary is a
   failed gate, not evidence that dependencies are safe.
