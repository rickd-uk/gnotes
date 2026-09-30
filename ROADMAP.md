# gnotes roadmap

Last reviewed: 2026-09-29

## Current production milestone

Production runs at `https://gnotes.rickd.dev` on the Kagoya VPS as a rootless Podman container behind Nginx and TLS, currently on `v0.7.4`. It uses SQLite in WAL mode, checksum-verified releases, health checks, pre-update validated backups, and automatic application rollback during failed updates. The older `hz-sin` systemd deployment is separate from the public site.

The current release includes:

- Isolated user accounts, administrator controls, signup policy, invitations, CSRF protection, and persistent login throttling.
- Autosaved drafts and edits, refresh recovery, pinning, hiding, recycling, date sections, archive overviews, pagination, and indexed full-text search.
- Recoverable note archiving, persistent note background colors, expanded appearance controls, selectable writing styles, and link underline preferences.
- Responsive density controls and persistent per-user interface state.
- Markdown formatting tools, fenced-code completion, brace pairing, and server-side syntax highlighting.
- A one-command release updater and operational checker.

## Next release: recovery tooling

The recovery milestone is a safe, tested restore workflow. The systemd and Podman commands, integration tests, and an isolated full application rehearsal are implemented. The Podman command and a pre-update backup were verified on Kagoya after the `v0.7.2` deployment.

1. [x] Add verification-only restore commands for systemd and the Kagoya Podman deployment.
2. [x] Validate gzip archives and run SQLite integrity and schema checks before accepting a backup.
3. [x] For a real restore, stop gnotes, preserve the current database, install the validated replacement atomically with correct permissions, restart the service, and verify health.
4. [x] Automatically restore the preserved database if the replacement cannot start cleanly.
5. [x] Add shell tests for valid, corrupt, incomplete, and failed-health restore scenarios.
6. [x] Document and perform a complete restore rehearsal using a temporary database and port.

Definition of done: restoring or rehearsing a backup requires one command, a corrupt backup cannot replace production, and failure leaves either the original database or a verified restored database available.

## Reliability and monitoring

After recovery tooling:

1. [x] Schedule validated daily backups on Kagoya.
2. Replicate them to encrypted storage outside the application server using Restic or an equivalent audited tool.
3. Alert when backups are stale or fail, disk space is low, TLS approaches expiry, the service repeatedly restarts, or public health checks fail.
4. Add privacy-conscious audit records for administrator actions and authentication security events, with a documented retention period.
5. Periodically test restoration instead of treating backup creation alone as proof of recoverability.

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

- **High priority: export notes.** Support exporting one, selected, or all notes in multiple formats, including readable Markdown and plain text plus a versioned, machine-readable gnotes format that can be imported later without losing note titles, content, dates, or other supported metadata. Provide a matching import path and define duplicate handling before release.
- [x] Add durable note archiving for one note or all active notes. Archived notes leave the main notes view but remain accessible in an Archived view, with individual restore. This is distinct from temporary Hide and from the recycle bin.
- Opt-in encrypted offline reading and editing with explicit synchronization state.
- Interactive task-checkbox toggling from rendered notes.
- Carefully scoped formatting improvements that keep the writing interface uncluttered.

Security requirements and operating procedures remain authoritative in [SECURITY.md](SECURITY.md) and [OPERATIONS.md](OPERATIONS.md).
