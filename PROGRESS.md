# gnotes progress

Last updated: 2026-10-04

## Current production release

`v0.7.15` is deployed on Kagoya at `https://gnotes.rickd.dev`.

- The release workflow passed Go tests, the race test, `go vet`, ShellCheck, backup and restore tests, and the application rehearsal.
- The Kagoya updater verified the release checksum, created a pre-release SQLite backup, restarted the rootless Podman service, and verified application health.
- The live container is healthy and SQLite is connected. The public page hash matches the packaged page, and the updater saved a validated pre-release backup.
- The `v0.7.12` release adds more rich-text formatting, interactive saved checklists, and named links. Its release workflow passed, and the deployed page hash matches the local release page.
- The `v0.7.13` release adds code-block language and normal-text controls in the rich editor. The public page and rich-editor asset hashes match the release files.
- The `v0.7.14` release keeps the code language picker open when entering a saved note. Its release workflow passed; the Kagoya updater made a validated pre-update backup, and the running binary and browser asset hashes match the release archive. Local and public health checks passed after deployment.
- The `v0.7.15` release adds a persistent rich-text formatting toolbar to new and saved notes. Its release workflow passed; the Kagoya updater made a validated pre-update backup, and the running binary and public page hashes match the release archive. Local and public health checks passed after deployment.
- Search → Dates and options now defaults Jump to date to today, removes the duplicate calendar picker button, and gives the section toggle larger, more prominent text.

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

- The rich editor now shows a persistent 14-command formatting toolbar in new and saved notes. A local 320px Chromium check verified toolbar visibility, no horizontal page overflow, Bold before typing, and Italic on selected text after reopening a saved note. Deployed in `v0.7.15`; an authenticated live browser check remains open.
- Opening a saved code block by clicking its preview no longer immediately closes the language picker. A 320px Chromium test reproduced the missing picker, then verified a real click shows the picker and a language change persists after saving. The correction is deployed in `v0.7.14`; an authenticated live browser check remains open.
- Rich-text code blocks now show a language picker at the cursor, including Plain and the existing source-editor language choices, plus a Normal text conversion. An existing custom language remains selectable. A 320px Chromium check covered picker fit, language changes, unknown-language preservation, plain code, saved highlighting, reopening, and persistence after editing an existing note. Go tests and vet passed. Deployed in `v0.7.13`.
- Rich text now adds Strikethrough and Inline code to the selected-text toolbar, plus Small heading, Quote, Code block, and Divider to `/`; Checklist is the first `/` option and also appears in the selection toolbar. Saved checklist boxes can be ticked directly in Recent notes and persist after reload, with failed saves rolled back. The Link control opens an in-app form for display text and address; saved links show the name, reveal the address on hover, and can be removed without losing their text. Focused 320px browser checks covered the new commands, menu fit, named-link saving/removal, and checkbox persistence/error rollback, including a task-looking line inside a code block. `go test ./...`, `go vet ./...`, and JavaScript syntax checks passed. Deployed in `v0.7.12`.
- The `v0.7.8` release workflow passed its Go, race, vet, ShellCheck, backup, restore, and application rehearsal checks. Kagoya made a validated pre-update backup; the deployed page hash matched the release and the public health endpoint passed.
- Local 320px browser checks covered the header, Controls row, welcome dialog, new-account “Don’t show this again” behavior, and visible note-color updates.
- Retest the deployed update on the Pixel 6a in Brave, Epic, Firefox, and Chrome. A prior Epic empty list was caused by a saved date filter; choosing Recent showed notes. No browser-specific storage failure was confirmed.
- Wasabi or other encrypted off-server backup storage is not configured. Daily validated backups currently remain on the Kagoya VPS.
- Export and import are included in v0.7.9. The new account panel supports all or selected notes, three export formats, and JSON, ZIP, Markdown, and text import. Round-trip and account-isolation tests pass.
- Rich text editing is included in v0.7.9 with a small toolbar for selected text, `/` block options, visible paragraph breaks, and Markdown source editing under Account → Advanced. The global font and size controls still apply. Older Markdown that cannot round-trip through the rich editor opens in source mode to preserve it. The global Hide eye was removed; per-note Hide and the Hidden notes panel remain. The controls allow 0–100% menu opacity and 65–175% title and text sizes. The same Save button appears on mobile and wider screens, note action buttons and icons are larger, and pinned note dates have left padding. Clicking a note opens editing near the clicked text position; Done closes the editor. The Edit icon is removed, and Copy is an icon in the same single-row note menu as the other actions. Page and displayed note text no longer select by dragging; text fields and rich editors still allow selection.
- The account menu now closes on sign-out and is never restored from saved appearance state. Existing browsers with the old saved open flag also start with the menu closed after sign-in.
- Local Chromium checks covered export and import, rich text typing and saving, paragraph breaks, checklists, selected-text and slash commands, editing existing notes, Advanced preference restoration, old Markdown preservation, appearance range endpoints, Save on 320px and 1024px screens, pinned date spacing, action menu fit without horizontal overflow, click-to-edit from title and body, link clicks that do not open editing, note text copy, and editor selection. In 320px Chromium touch emulation, a swipe did not open editing and the following tap did. `go test ./...`, `go vet ./...`, and JavaScript syntax validation passed. The Pixel 6a was not connected, so the four-browser device retest remains open.
- A 320px Chromium sign-out/sign-in check confirmed the account menu stays closed, including with a legacy saved `accountOpen: true` value and after reloading while the menu was open.
- A 320px Chromium check confirmed the six action icons stay on one row without horizontal overflow, the Copy icon copies note text, and title and multi-paragraph body clicks place the editing caret in the clicked text and paragraph.
- Rich-text links turn typed or pasted `[label](URL)` into a labeled hyperlink. Untitled note previews use the label rather than showing the Markdown syntax. Chromium checks covered typing, pasting one or two links, text after a link, invalid URLs, saving, and reopening a saved link.
- Note cards show up to two chosen quick actions beside the More button, defaulting to Archive and Delete. Controls lets each browser choose from Archive, Delete, Pin, Hide, Color, and Copy; all remaining actions stay in More. Delete offers an eight-second Undo button. Chromium checks at 320px, 375px, and 1024px covered Full, Compact, and Titles layouts, action selection and persistence, menu fit, color, delete, undo, and archive.
- The header archive icon toggles between Recent and a full Archived notes view, with a tinted active icon, a distinct banner, and the same note cards and density choices. Archived cards show their rendered content and archive date, with Unarchive and Copy actions; editing remains in Recent after unarchiving. The archive view uses 50-note pages, an archive paging index, and archive-aware search/date filters. Chromium checks at 320px and 1024px covered mode switching, card rendering, Titles layout, search, and unarchive. A Go test traversed 125 archived notes, checked same-time cursor ties, user isolation, search paging, and archive-date filtering.
- Archive and unarchive show a brief in-app success toast and a styled dialog for failures. Page responses identify Recent or Archived; the client rejects an archive response from a stale local server (or a missing archive date) with a restart message instead of showing "Unknown date" and offering invalid unarchive actions. Go tests, vet, JavaScript syntax checks, the 320px/1024px archive browser run, and a focused browser check of success, failure, and a simulated stale server response passed.

## Future plans

Priority order remains:

1. Retest the mobile experience and note colors on the Pixel 6a, including Brave and Epic, and fix any reproducible issues.
2. Replicate validated backups to encrypted storage outside Kagoya and alert on stale or failed backups, low disk space, service restarts, and TLS expiry.
3. Retest the new export/import and rich text controls on the Pixel 6a after the v0.7.13 release.
4. Move inline JavaScript and CSS into versioned assets and remove `unsafe-inline` from the Content Security Policy.
5. Add privacy-conscious audit records for administration and authentication security events.
6. Design password recovery and administrator MFA with recovery codes before implementation.
7. Add bulk archive and restore controls, then consider encrypted offline access and task-checkbox interaction.

End-to-end encryption, horizontal scaling, and a PostgreSQL migration remain later design projects. SQLite remains appropriate for the current single-instance deployment.
