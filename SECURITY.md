# Security and reliability

This document defines the deployment baseline for gnotes. No internet-facing service is “completely secure”; production safety depends on the application, the host, the reverse proxy, monitoring, backups, and operating procedures all remaining healthy.

## Current protections

- Notes, drafts, and destructive operations are scoped to the authenticated user in database queries.
- Session tokens and CSRF tokens use 256 bits of operating-system randomness. Only a SHA-256 hash of each session token is stored.
- Session cookies are `HttpOnly` and `SameSite=Strict`; the production environment enables `Secure` cookies and HSTS.
- Passwords use bcrypt cost 12. Older bcrypt hashes are upgraded after a successful login.
- Optional Brevo recovery requires a verified email and uses hashed, single-use links: verification expires in one hour and reset in 30 minutes. Requests have persistent rate limits and generic replies; resets revoke sessions and other links. Password confirmation is required to set a recovery email. Email links use a configured origin and fragments so tokens stay out of access logs.
- Login and registration limits persist in SQLite across restarts. Login failures trigger an account-level exponential cooldown, and forwarded client addresses are accepted only from a loopback reverse proxy.
- Registration supports hashed single-use invitation codes plus configurable global and per-IP daily caps.
- New installations allow creation of the first `rick` administrator, then leave further signups closed until the administrator enables them.
- Request, title, note-body, search, and session-count limits constrain accidental or malicious resource use.
- Markdown is rendered without raw active HTML, and browser security headers restrict scripts, framing, object embedding, referrers, and sensitive browser features.
- SQLite uses one in-process connection, a five-second busy timeout, WAL journaling, full synchronous writes, and restrictive file permissions.
- The systemd unit runs without root privileges or Linux capabilities and receives write access only to the database directory.
- The Kagoya updater makes a validated online SQLite backup before each release. A user timer creates validated daily backups on that host.

Default application limits are 20 login requests per IP and 500 globally per 15 minutes, plus 10 registration requests per IP and 100 globally per hour. Five failed passwords for one username begin an escalating cooldown from 30 seconds up to 15 minutes. Successful registrations default to 20 globally and 3 per IP per UTC day; the administrator can lower these values and require a single-use invitation.

## Production launch checklist

- [ ] Bind gnotes to `127.0.0.1`; expose only Nginx ports 80 and 443.
- [ ] Keep Nginx as the only direct proxy. If a CDN is added, configure Nginx's trusted real-IP ranges before changing forwarded-header handling.
- [ ] Use a real domain and valid TLS certificate; keep `GNOTES_SECURE_COOKIES=true`.
- [ ] Use a long random `GNOTES_SETUP_TOKEN` for remote bootstrap, then remove it and restart gnotes.
- [ ] Verify public signups are off unless they are deliberately needed.
- [ ] Restrict SSH to keys, disable direct root login, enable the host firewall, and install unattended security updates.
- [ ] Keep `/etc/gnotes/gnotes.env` readable only by `root:gnotes` and `/var/lib/gnotes` readable only by `gnotes`.
- [ ] Run `systemd-analyze verify` against all units on the target Ubuntu release.
- [ ] Confirm the health endpoint through HTTPS and inspect `journalctl -u gnotes.service`.
- [ ] Run the backup job manually and perform a restore rehearsal before accepting real data.
- [ ] Replicate encrypted backups to a different provider or failure domain and alert when backups stop arriving.
- [ ] Monitor disk space, HTTP error rate, certificate expiry, service restarts, and health-check failures.

## Restore rehearsal

Use `restore-gnotes --verify BACKUP.db.gz` to validate a backup without touching the service. Rehearse a real restore on a disposable host or isolated instance with separate database and service paths, following [OPERATIONS.md](OPERATIONS.md). Sign in and inspect notes, drafts, recycle-bin entries, and administrator settings. The restore command preserves the previous database and rolls it back if the restored service fails its health check. It does not prove that every note is present, so browser inspection remains necessary.

## Incident response

If compromise is suspected:

1. Remove the service from public access without deleting the server or database.
2. Snapshot the server and preserve Nginx, SSH, and systemd logs.
3. Rotate SSH credentials, TLS private keys when relevant, and all user passwords. Revoking all sessions requires deleting the rows from the `sessions` table while the service is stopped.
4. Restore only from a validated backup known to predate the compromise.
5. Patch the root cause before restoring public access.

## Important limitations

Notes are not yet end-to-end encrypted. Filesystem permissions and host encryption protect a lost disk, but an attacker who obtains the live database or compromises the running server can read note titles and contents. Backups contain the same plaintext and must be encrypted before being copied off-server.

The current rate limiter is suitable for one SQLite-backed instance, not a horizontally scaled deployment. Hashed IP identifiers still count as sensitive operational data and are retained for request limits and signup policy. There is no MFA, and JavaScript/CSS remain inline under the Content Security Policy. Email recovery depends on access to the verified mailbox and on Brevo; it is unavailable without server configuration. End-to-end encryption requires a deliberate recovery-key and multi-device design; adding only server-side database encryption would not protect against an attacker controlling the running server.

## Reporting a vulnerability

Do not include real note contents, passwords, session cookies, setup tokens, or private keys in an issue or log excerpt. Contact the operator privately with reproduction steps and sanitized evidence.
