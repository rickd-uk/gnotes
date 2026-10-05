# gnotes roadmap

Last reviewed: 2026-10-05

## Current production milestone

Production runs at `https://gnotes.rickd.dev` on the Kagoya VPS as a rootless Podman container behind Nginx and TLS, currently on `v0.7.22`. It uses SQLite in WAL mode, checksum-verified releases, health checks, pre-update validated backups, and automatic application rollback during failed updates. The older `hz-sin` systemd deployment is separate from the public site.

The current release includes:

- Isolated user accounts, administrator controls, signup policy, invitations, CSRF protection, and persistent login throttling.
- Autosaved drafts and edits, refresh recovery, pinning, hiding, recycling, date sections, archive overviews, pagination, and indexed full-text search.
- Recoverable note archiving, persistent note background colors, expanded appearance controls, selectable writing styles, and link underline preferences.
- Responsive density controls and persistent per-user interface state.
- Mobile Save control, swipe-revealed note actions, direct Archived access in the header, and a welcome dialog after account creation.
- A mobile Controls row for Full, Fold, and Notes; Hide remains in Controls. The welcome dismissal preference is browser-local.
- Rich text editing with Markdown source under Advanced, export and import, full Archived notes view, configurable quick actions, and archive feedback.
- Markdown fenced-code completion, brace pairing, syntax highlighting, and clickable code-language labels.
- Verified-email password recovery, note roll-up, selectable time zones and text, note titles in browser tabs, and compact view controls.
- A one-command release updater and operational checker.

## Recovery tooling

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

The notification provider and off-server storage destination require an explicit deployment choice before implementation. Wasabi is a possible destination, but no Wasabi backup is currently configured.

## Mobile verification

1. Retest v0.7.22 with an authenticated account on the Pixel 6a in Brave, Epic, Firefox, and Chrome: create and save notes, reopen them in the focused editor, use Copy, the header Archive control, archive removal for one note, a date, and all notes, then recover a removed note from the recycle bin. Check swipe actions, note colors, the Aa formatting toggle, green completion checks, three quick actions, and the code language picker when editing a saved note.
2. Check the welcome dialog on a newly created test account without touching existing notes, including Help links and the dismissal choice.
3. Investigate browser-specific failures only when reproduced. Epic previously displayed an empty list because a saved date filter was active; Recent restored the notes.

## Browser and account security

1. Move inline JavaScript and CSS into versioned static files, then remove `unsafe-inline` from the Content Security Policy.
2. Add user data export and complete self-service account deletion.
3. [x] Configure Brevo recovery in production with verified recovery emails and a private API key. Live admin setup and real inbox reset confirmation remain open.
4. Add administrator MFA and recovery codes.

## Offline access and encryption

Offline storage must be opt-in and encrypted locally. Private notes should not be copied into IndexedDB or a service-worker cache in plaintext, especially on shared or stolen devices.

End-to-end encryption needs a separate design covering recovery keys, multiple devices, password changes, offline conflict resolution, client-side search, and the inability of an administrator to recover forgotten encrypted data. Server-side SQLite encryption alone does not protect notes from an attacker controlling the running server.

An independently trusted client is required if the threat model includes an actively compromised web server serving malicious JavaScript.

## Scaling decisions

SQLite remains appropriate for the current modest, single-instance deployment. Do not operate multiple writable application instances against independent SQLite databases. Consider PostgreSQL and shared rate limiting only when horizontal scaling becomes necessary.

## Product backlog

- [x] Export and import notes. Account-menu export supports one, selected, or all notes as readable Markdown or plain text, or versioned gnotes JSON. Multi-note readable exports include a JSON manifest for lossless reimport. Import supports all three formats and the ZIP archive; duplicate handling is explicit. Device review remains open.
- [x] Rich text editing. Selecting text offers bold, italic, and link; typing `/` at the start of a paragraph offers headings, lists, and checklists. Markdown source is available through Account → Advanced. Existing global font and size settings remain available. Device review remains open before expanding formatting tools.
- [x] Add durable note archiving for one note or all active notes. Archived notes leave the main notes view but remain accessible in an Archived view, with individual restore. This is distinct from temporary Hide and from the recycle bin.
- Opt-in encrypted offline reading and editing with explicit synchronization state.
- [x] Interactive task-checkbox toggling from rendered notes.
- [x] Account-specific note tags with filtering and search; implemented locally, awaiting release.
- Carefully scoped formatting improvements that keep the writing interface uncluttered.

### Distant ideas

- Add editable tables in the rich editor, including row, column, and cell controls, while preserving readable Markdown storage and exports.
- Explore grids after defining whether they should arrange note cards or provide spreadsheet-style cells.
- Explore graphs and charts generated from note data, with a clear source format and safe rendering.
- Explore attaching documents to notes and previewing supported files in place. Define per-account access, file size and type limits, private storage, export behavior, and backup coverage before implementation.
- Explore a personal dictionary: right-click a word in a note to save it to an account-specific word list, view definitions, and manage saved entries. Choose a freely licensed dictionary dataset that can be stored and searched on the server with no runtime online lookups; review its license, update process, and backup size before implementation.

Security requirements and operating procedures remain authoritative in [SECURITY.md](SECURITY.md) and [OPERATIONS.md](OPERATIONS.md).
