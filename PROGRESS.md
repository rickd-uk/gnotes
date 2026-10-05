# gnotes status

Updated: 2026-10-05

## Production

`v0.7.21` is deployed at https://gnotes.rickd.dev on Kagoya with rootless Podman and SQLite. It adds archive removal for one note, one displayed date, or all archived notes.

Release checks passed, including Go/race/vet, ShellCheck, backup/restore tests, and the application rehearsal. Deployment verified checksums, a validated pre-update backup, application health, and the public page hash.

## Local work awaiting release

- Per-note body roll-up, remembered per account in this browser.
- Density, Fold/Open, and Tools On/Off controls above the notes.
- Selectable time zone for note times, date groups, searches, and archive date actions; active date and text searches survive zone changes.
- Brevo recovery is enabled locally. Verification mail arrived and the admin recovery email is confirmed; real reset-mail testing and production configuration remain open. Sign-in/main-screen branding matches.
- Browser tab titles, clickable code-language labels, corrected checklist layout, text selection, shorter timestamps, and bold formatting boundaries.
- Release candidate `v0.7.22`: Go/race/vet, ShellCheck, backup/restore tests, and restore rehearsal passed. Chromium at 320px/1024px verified recovery, branding, roll-up, density, time-zone persistence, tab titles, code editing, and mouse selection; keyboard checks verified bold boundaries.
- Local server: http://127.0.0.1:8080, using the existing local database.

## Next steps

1. Configure Brevo in production and deploy `v0.7.22`, then confirm real reset-email delivery. Retest on the Pixel 6a in Brave, Epic, Firefox, and Chrome: recovery, save/reopen, focused editor and Copy, Aa/formatting, quick actions, code language picker, archive removal/recovery, export/import, and welcome behavior. Epic previously showed an empty list because of a saved date filter; no browser storage failure was confirmed.
2. Choose encrypted off-server backup storage and an alert provider. Validated daily backups currently remain on Kagoya; add replication and alerts for backup failures, disk space, restarts, TLS expiry, and public health.
3. Continue security work: remove inline assets from CSP, add audit records, and design administrator MFA.

Feature documentation: [README.md](README.md). Backlog: [ROADMAP.md](ROADMAP.md). Operations: [OPERATIONS.md](OPERATIONS.md). Earlier release and verification details remain in Git history.
