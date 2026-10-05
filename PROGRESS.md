# gnotes status

Updated: 2026-10-05

## Production

`v0.7.22` is live at https://gnotes.rickd.dev on Kagoya with rootless Podman and SQLite.

This release adds Brevo password recovery, per-note roll-up, compact view controls, selectable time zones, note titles in browser tabs, clickable code-language labels, corrected checklists, selectable note text, shorter timestamps, and bold formatting boundaries.

Go/race/vet, ShellCheck, backup/restore tests, the restore rehearsal, and GitHub release checks passed. Chromium checks covered 320px/1024px layouts, recovery, time-zone persistence, editing, mouse selection, and bold typing. Deployment verified archive/binary/page checksums, a pre-update backup, public health, database integrity, and preservation of 4 accounts and 31 notes.

Brevo authenticated from Kagoya and recovery is enabled. The live admin must verify a recovery email in Profile; local verification does not carry over. Real reset-email inbox confirmation remains open.

## Next work

1. Add note tags and filtering. Retest the release on the Pixel 6a browsers, including recovery, code editing, archive removal, export/import, and the welcome dialog.
2. Choose encrypted off-server backup storage and alerts; validated daily backups currently remain on Kagoya.
3. Tighten CSP, add audit records, and design administrator MFA.

Local server: http://127.0.0.1:8080. Backlog: [ROADMAP.md](ROADMAP.md). Operations: [OPERATIONS.md](OPERATIONS.md). Earlier details remain in Git history.
