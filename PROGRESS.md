# gnotes status

Updated: 2026-10-05

## Production

`v0.7.23` is live at https://gnotes.rickd.dev on Kagoya with rootless Podman and SQLite.

This release adds private note tags, combined tag/text/date search, persistent filters, and tag-preserving export/import. It also improves the tags dialog and corrects Markdown heading positioning.

Go/race/vet, ShellCheck, backup/restore tests, the restore rehearsal, and GitHub release checks passed. Chromium checks covered tags, editing, recovery, and 320px/1024px layouts. Deployment verified checksums, a pre-update backup, public health, protected tag endpoints, database integrity, and preservation of 4 accounts and 31 notes.

Brevo authenticated from Kagoya and recovery is enabled. The live admin must verify a recovery email in Profile; local verification does not carry over. Real reset-email inbox confirmation remains open.

## Next work

1. Retest `v0.7.23` on the Pixel 6a browsers, including tags, recovery, code editing, archive removal, export/import, and the welcome dialog.
2. Choose encrypted off-server backup storage and alerts; validated daily backups currently remain on Kagoya.
3. Tighten CSP, add audit records, and design administrator MFA.

Local server: http://127.0.0.1:8080. Backlog: [ROADMAP.md](ROADMAP.md). Operations: [OPERATIONS.md](OPERATIONS.md). Earlier details remain in Git history.
