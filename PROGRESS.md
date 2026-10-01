# gnotes progress

Last updated: 2026-10-01

## Current production release

`v0.7.8` is deployed on Kagoya at `https://gnotes.rickd.dev`.

- The release workflow passed Go tests, the race test, `go vet`, ShellCheck, backup and restore tests, and the application rehearsal.
- The Kagoya updater verified the release checksum, created a pre-release SQLite backup, restarted the rootless Podman service, and verified application health.
- The live container is healthy and SQLite is connected.

## Recently completed

- Full, Fold, and Notes share a row in the wide-screen Tools menu.
- Note titles share their row with the note time and action menu.
- Title and text controls now range from 75% to 150%.
- Note font choices include Writer, Balanced, Clean, Cursive, Technical, and Playful.
- Menu opacity can be lowered to 15%.
- Notes support persistent subtle background colors with readable contrasting text.
- Notes can be archived for later reading and restored from Archived notes without deletion.
- Link underlines can be turned on or off; color-only links underline on hover and focus.
- The administration menu label is shorter: Users & signups.
- Small-screen notes have side padding and titles below the timestamp and actions row.
- The mobile composer has a compact, pale-green **Save** button and subtle visual confirmation after a note is saved. Drafts still autosave.
- Swipe left on a mobile note to reveal its actions, then tap an action; swipe right to close them.
- Archived notes is directly accessible from the header. Hide remains a browser-local control in the Controls drawer; Archive persists with the account.
- Full, Fold, and Notes share one row in the mobile Controls drawer.
- New accounts see a brief welcome dialog with first-note instructions and Help and Markdown guide links. Its “Don’t show this again” choice is stored per account in the current browser.
- Note color swatches are larger. A selected color updates the card after the server confirms it, and the newest-note highlight no longer masks the chosen background.
- Empty filtered results offer a return to Recent, and note and draft load or save errors are clearer.

## Verification and open checks

- The `v0.7.8` release workflow passed its Go, race, vet, ShellCheck, backup, restore, and application rehearsal checks. Kagoya made a validated pre-update backup; the deployed page hash matched the release and the public health endpoint passed.
- Local 320px browser checks covered the header, Controls row, welcome dialog, new-account “Don’t show this again” behavior, and visible note-color updates.
- Retest the deployed update on the Pixel 6a in Brave, Epic, Firefox, and Chrome. A prior Epic empty list was caused by a saved date filter; choosing Recent showed notes. No browser-specific storage failure was confirmed.
- Wasabi or other encrypted off-server backup storage is not configured. Daily validated backups currently remain on the Kagoya VPS.

## Future plans

Priority order remains:

1. Retest the mobile experience and note colors on the Pixel 6a, including Brave and Epic, and fix any reproducible issues.
2. Replicate validated backups to encrypted storage outside Kagoya and alert on stale or failed backups, low disk space, service restarts, and TLS expiry.
3. Add note export and import with Markdown, plain text, and a versioned lossless gnotes format.
4. Move inline JavaScript and CSS into versioned assets and remove `unsafe-inline` from the Content Security Policy.
5. Add privacy-conscious audit records for administration and authentication security events.
6. Design password recovery and administrator MFA with recovery codes before implementation.
7. Add bulk archive and restore controls, then consider encrypted offline access and task-checkbox interaction.

End-to-end encryption, horizontal scaling, and a PostgreSQL migration remain later design projects. SQLite remains appropriate for the current single-instance deployment.
