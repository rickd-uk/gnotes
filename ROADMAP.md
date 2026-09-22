# gnotes roadmap

Last reviewed: 2026-09-22

## Current production milestone

`v0.6.0` is deployed at `https://gnotes.futureisnear.dev` on a single Ubuntu server behind Nginx and TLS. The application runs under systemd with SQLite in WAL mode, daily validated local backups, checksum-verified releases, health checks, and automatic application rollback during failed updates.

The current release includes:

- Isolated user accounts, administrator controls, signup policy, invitations, CSRF protection, and persistent login throttling.
- Autosaved drafts and edits, refresh recovery, pinning, hiding, recycling, date sections, archive overviews, pagination, and indexed full-text search.
- Responsive density controls and persistent per-user interface state.
- Markdown formatting tools, fenced-code completion, brace pairing, and server-side syntax highlighting.
- A one-command release updater and operational checker.

## Next release: recovery tooling

The recommended scope for `v0.7.0` is a safe, tested restore workflow.

1. Add `/opt/gnotes/bin/restore-gnotes` with a verification-only mode that never modifies production.
2. Validate gzip archives and run SQLite integrity and schema checks before accepting a backup.
3. For a real restore, stop gnotes, preserve the current database, install the validated replacement atomically with correct ownership and permissions, restart the service, and verify health.
4. Automatically restore the preserved database if the replacement cannot start cleanly.
5. Add shell tests for valid, corrupt, incomplete, and failed-health restore scenarios.
6. Document and perform a complete restore rehearsal using a temporary database and port.

Definition of done: restoring or rehearsing a backup requires one command, a corrupt backup cannot replace production, and failure leaves either the original database or a verified restored database available.

## Reliability and monitoring

After recovery tooling:

1. Replicate backups to encrypted storage outside the application server using Restic or an equivalent audited tool.
2. Alert when backups are stale or fail, disk space is low, TLS approaches expiry, the service repeatedly restarts, or public health checks fail.
3. Add privacy-conscious audit records for administrator actions and authentication security events, with a documented retention period.
4. Periodically test restoration instead of treating backup creation alone as proof of recoverability.

The notification provider and off-server storage destination require an explicit deployment choice before implementation.

## Browser and account security

1. Move inline JavaScript and CSS into versioned static files, then remove `unsafe-inline` from the Content Security Policy.
2. Add user data export and complete self-service account deletion.
3. Design password recovery without weakening note privacy.
4. Add administrator MFA and recovery codes.

## Offline access and encryption

Offline storage must be opt-in and encrypted locally. Private notes should not be copied into IndexedDB or a service-worker cache in plaintext, especially on shared or stolen devices.

End-to-end encryption needs a separate design covering recovery keys, multiple devices, password changes, offline conflict resolution, client-side search, and the inability of an administrator to recover forgotten encrypted data. Server-side SQLite encryption alone does not protect notes from an attacker controlling the running server.

An independently trusted client is required if the threat model includes an actively compromised web server serving malicious JavaScript.

## Scaling decisions

SQLite remains appropriate for the current modest, single-instance deployment. Do not operate multiple writable application instances against independent SQLite databases. Consider PostgreSQL and shared rate limiting only when horizontal scaling becomes necessary.

## Product backlog

- Opt-in encrypted offline reading and editing with explicit synchronization state.
- Interactive task-checkbox toggling from rendered notes.
- Import and export in Markdown and a machine-readable archive format.
- Carefully scoped formatting improvements that keep the writing interface uncluttered.

Security requirements and operating procedures remain authoritative in [SECURITY.md](SECURITY.md) and [OPERATIONS.md](OPERATIONS.md).
