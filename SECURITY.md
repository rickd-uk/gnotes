# Security and reliability

This document defines the deployment baseline for gnotes. No internet-facing service is “completely secure”; production safety depends on the application, the host, the reverse proxy, monitoring, backups, and operating procedures all remaining healthy.

## Current protections

- Notes, drafts, and destructive operations are scoped to the authenticated user in database queries.
- Session tokens and CSRF tokens use 256 bits of operating-system randomness. Only a SHA-256 hash of each session token is stored.
- Session cookies are `HttpOnly` and `SameSite=Strict`; the production environment enables `Secure` cookies and HSTS.
- Passwords use bcrypt cost 12. Older bcrypt hashes are upgraded after a successful login.
- Login and registration attempts are bounded in memory. Forwarded client addresses are accepted only from a loopback reverse proxy.
- New installations allow creation of the first `rick` administrator, then leave further signups closed until the administrator enables them.
- Request, title, note-body, search, and session-count limits constrain accidental or malicious resource use.
- Markdown is rendered without raw active HTML, and browser security headers restrict scripts, framing, object embedding, referrers, and sensitive browser features.
- SQLite uses one in-process connection, a five-second busy timeout, WAL journaling, full synchronous writes, and restrictive file permissions.
- The systemd unit runs without root privileges or Linux capabilities and receives write access only to the database directory.
- Daily backups use SQLite's online backup operation, pass `PRAGMA quick_check`, and are compressed only after validation.

## Production launch checklist

- [ ] Bind gnotes to `127.0.0.1`; expose only Nginx ports 80 and 443.
- [ ] Use a real domain and valid TLS certificate; keep `GNOTES_SECURE_COOKIES=true`.
- [ ] Use a long random `GNOTES_SETUP_TOKEN` for remote bootstrap, then remove it and restart gnotes.
- [ ] Verify public signups are off unless they are deliberately needed.
- [ ] Restrict SSH to keys, disable direct root login, enable the host firewall, and install unattended security updates.
- [ ] Keep `/etc/gnotes/gnotes.env` readable only by `root:gnotes` and `/var/lib/gnotes` readable only by `gnotes`.
- [ ] Run `systemd-analyze verify` against all units on the target Ubuntu release.
- [ ] Confirm the health endpoint through HTTPS and inspect `journalctl -u gnotes.service`.
- [ ] Run the backup unit manually and perform a restore rehearsal before accepting real data.
- [ ] Replicate encrypted backups to a different provider or failure domain and alert when backups stop arriving.
- [ ] Monitor disk space, HTTP error rate, certificate expiry, service restarts, and health-check failures.

## Restore rehearsal

Never overwrite the production database while gnotes is running. Restore into a separate staging path first:

```bash
gzip -dc /var/backups/gnotes/gnotes-YYYYMMDDTHHMMSSZ.db.gz > /tmp/gnotes-restore-test.db
sqlite3 /tmp/gnotes-restore-test.db 'PRAGMA integrity_check;'
DATABASE_PATH=/tmp/gnotes-restore-test.db HOST=127.0.0.1 PORT=8081 ./gnotes
```

Sign in on the temporary instance and inspect several notes, drafts, recycle-bin entries, and administrator settings. Stop it and remove the temporary database after the rehearsal. A real restore should stop `gnotes.service`, preserve the damaged database for investigation, install the validated replacement with owner `gnotes:gnotes` and mode `0600`, then start the service and inspect its logs.

## Incident response

If compromise is suspected:

1. Remove the service from public access without deleting the server or database.
2. Snapshot the server and preserve Nginx, SSH, and systemd logs.
3. Rotate SSH credentials, TLS private keys when relevant, and all user passwords. Revoking all sessions requires deleting the rows from the `sessions` table while the service is stopped.
4. Restore only from a validated backup known to predate the compromise.
5. Patch the root cause before restoring public access.

## Important limitations

Notes are not yet end-to-end encrypted. Filesystem permissions and host encryption protect a lost disk, but an attacker who obtains the live database or compromises the running server can read note titles and contents. Backups contain the same plaintext and must be encrypted before being copied off-server.

The current rate limiter is process-local, there is no MFA or password-reset flow, and JavaScript/CSS remain inline under the Content Security Policy. These are recorded in the README roadmap. End-to-end encryption requires a deliberate recovery-key and multi-device design; adding only server-side database encryption would not protect against an attacker controlling the running server.

## Reporting a vulnerability

Do not include real note contents, passwords, session cookies, setup tokens, or private keys in an issue or log excerpt. Contact the operator privately with reproduction steps and sanitized evidence.
