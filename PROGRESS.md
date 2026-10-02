# gnotes progress

Last updated: 2026-10-02

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
- Export and import are implemented locally after v0.7.8. The new account panel supports all or selected notes, three export formats, and JSON, ZIP, Markdown, and text import. Round-trip and account-isolation tests pass; this work has not been deployed.
- Rich text editing is implemented locally after v0.7.8 with a small toolbar for selected text, `/` block options, visible paragraph breaks, and Markdown source editing under Account → Advanced. The global font and size controls still apply. Older Markdown that cannot round-trip through the rich editor opens in source mode to preserve it. The global Hide eye was removed; per-note Hide and the Hidden notes panel remain. The local controls now allow 0–100% menu opacity and 65–175% title and text sizes. The same Save button now appears on mobile and wider screens, note action buttons and icons are larger, and pinned note dates have left padding. Clicking a note opens editing near the clicked text position; Done closes the editor. The Edit icon is removed, and Copy is an icon in the same single-row note menu as the other actions. Page and displayed note text no longer select by dragging; text fields and rich editors still allow selection. This work has not been deployed.
- The account menu now closes on sign-out and is never restored from saved appearance state. Existing browsers with the old saved open flag also start with the menu closed after sign-in.
- Local Chromium checks covered export and import, rich text typing and saving, paragraph breaks, checklists, selected-text and slash commands, editing existing notes, Advanced preference restoration, old Markdown preservation, appearance range endpoints, Save on 320px and 1024px screens, pinned date spacing, action menu fit without horizontal overflow, click-to-edit from title and body, link clicks that do not open editing, note text copy, and editor selection. In 320px Chromium touch emulation, a swipe did not open editing and the following tap did. `go test ./...`, `go vet ./...`, and JavaScript syntax validation passed. The Pixel 6a was not connected, so the four-browser device retest remains open.
- A 320px Chromium sign-out/sign-in check confirmed the account menu stays closed, including with a legacy saved `accountOpen: true` value and after reloading while the menu was open.
- A 320px Chromium check confirmed the six action icons stay on one row without horizontal overflow, the Copy icon copies note text, and title and multi-paragraph body clicks place the editing caret in the clicked text and paragraph.

## Future plans

Priority order remains:

1. Retest the mobile experience and note colors on the Pixel 6a, including Brave and Epic, and fix any reproducible issues.
2. Replicate validated backups to encrypted storage outside Kagoya and alert on stale or failed backups, low disk space, service restarts, and TLS expiry.
3. Review and release the local export/import and rich text changes after device testing.
4. Move inline JavaScript and CSS into versioned assets and remove `unsafe-inline` from the Content Security Policy.
5. Add privacy-conscious audit records for administration and authentication security events.
6. Design password recovery and administrator MFA with recovery codes before implementation.
7. Add bulk archive and restore controls, then consider encrypted offline access and task-checkbox interaction.

End-to-end encryption, horizontal scaling, and a PostgreSQL migration remain later design projects. SQLite remains appropriate for the current single-instance deployment.
