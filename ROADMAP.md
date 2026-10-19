# gnotes roadmap

Last reviewed: 2026-10-19

## Next development sequence

Session handoff and future plans saved on 2026-10-16 in [PROGRESS.md](PROGRESS.md). Changes through v0.7.61 completed and validated on 2026-10-17 improve note-list rendering, spelling, Markdown serialization, text menus, latest-query search cancellation and on-demand note editor controls; ordinary normal-editor indentation now also stays as text through saving and reopening. Changes from the v0.7.57–v0.7.61 checkpoints are now pushed and deployed together as v0.7.61. The follow-up v0.7.62 fix keeps actual editing context menus native across browsers to avoid the second async-clipboard Paste confirmation; local and live browser checks pass and it is deployed. The v0.7.63 follow-up treats literal Markdown indentation as text in rendering and normal-editor loading, including older notes and Vim/source output; explicit fenced code is retained. Local and live validation passed and v0.7.63 is deployed. The next speed candidate is avoiding redundant unchanged saves. The earlier editor, shortcut and note-lock features are complete. The requested **N** toggle now provides browser Vim editing for drafts and existing notes, with preserved autosave/preview/recovery and per-account browser preferences. Recommended next step is Pixel 6a testing and fixing reproduced friction, followed by backups/alerts and browser security; editable tables remain a later feature candidate. Proposed priorities:

1. Verify the current release on the Pixel 6a, including selection, keyboard/editor controls, menus, shortcuts, note locking and recovery. Check offline setup, disconnected reading and manual refresh on a real device too.
2. Finish encrypted off-server backup activation through the existing wizard; choose an alert destination and monitor stale/failed backups and service health. Budget occasional full integrity and restore checks for their storage egress.
3. Extract inline scripts/styles into versioned assets and tighten Content Security Policy.
4. Complete account data export and self-service account deletion after reviewing existing coverage and retention behavior.
5. Add administrator MFA/recovery codes and privacy-conscious audit logging with documented retention.
6. Consider scoped editor additions such as editable tables after reliability and security work.

Also verify the recovery email and a real password-reset email. Offline notes remain read only with manual snapshot refresh; offline editing/synchronization is not planned in this handoff. These are recorded future priorities, not instructions to begin implementation.

The operational Wasabi egress fix is installed on Kagoya and included in v0.7.55. Daily replication checks local database integrity and repository metadata without routine payload downloads. Explicit setup tests retain latest-snapshot restore verification. Automatic Wasabi scheduling remains disabled pending setup, and future full integrity/restore tests need an egress budget.

## Current production milestone

Production runs at `https://gnotes.rickd.dev` on the Kagoya VPS as a rootless Podman container behind Nginx and TLS, currently on `v0.8.0-rc.3`. It uses SQLCipher-encrypted SQLite in WAL mode, encrypted retained local backups, an owner-supplied RAM-only key, checksum-verified releases, health checks, pre-update validated backups, and automatic application rollback during failed updates. Rollback images must support encryption. The older `hz-sin` systemd deployment is separate from the public site.

The current release includes:

- Optional browser Vim editing through N, ordinary indentation preserved through normal-editor saves, faster incremental rendering/spelling and deferred serialization, lazy editor controls and latest-query cancellation.
- Isolated user accounts, administrator controls, signup policy, invitations, CSRF protection, and persistent login throttling.
- Autosaved drafts and edits, refresh recovery, pinning, hiding, recycling, date sections, archive overviews, pagination, and indexed full-text search.
- Recoverable note archiving, persistent note background colors, expanded appearance controls, selectable writing styles, and link underline preferences.
- Responsive density controls and persistent per-user interface state.
- Mobile Save control, swipe-revealed note actions, direct Archived access in the header, and a welcome dialog after account creation.
- A mobile Controls row for Full, Fold, and Notes; Hide remains in Controls. The welcome dismissal preference is browser-local.
- Rich text editing with Markdown source under Advanced, export and import, full Archived notes view, configurable quick actions, and archive feedback.
- Markdown fenced-code completion, brace pairing, syntax highlighting, and clickable code-language labels.
- Verified-email password recovery, note roll-up, selectable time zones and text, note titles in browser tabs, and compact view controls.
- Private note tags, combined tag/text/date search, persistent filters, and tag-preserving export/import.
- Compact tag displays show three tags and an expandable overflow count; all tags remain searchable and editable.
- Unhide appears on the left above notes beside the density controls when notes are hidden, with individual and all-note restoration.
- Personal dictionary with bundled English WordNet 3.1 definitions, right-click and selection lookup, private saved words, filtering, removal, and word-list export. No runtime external lookups; see [DICTIONARY.md](DICTIONARY.md) for licensing, updates, and backup coverage.
- Right-click Copy/Dictionary/Always ignore menu, native menu via Shift + right-click, spelling underline toggle, and separate account-owned ignored-word and Names lists in SQLite. Names support people, companies, places, and other names; matching rich note text is ignored automatically without changing note content. Older browser lists migrate automatically, and database backups cover both lists.
- Searchable personal Names and ignored-word lists, displayed in batches of 50. JSON export preserves name types and includes complete lists; JSON and one-entry-per-line text imports merge atomically without changing existing entries. Failed imports retain the selected file for retry. Files may be up to 2 MB, with 2,000 entries per personal list.
- Confirmed active-note bulk removal by displayed date section, Pinned, or all active notes. Date sections exclude pinned notes; removal covers hidden notes and later pages, preserves Archive, and is recoverable from the recycle bin.
- Accessible Archive all and Recycle all icons below active notes, bordered Cancel buttons, and consistent outside-click dismissal for all modal dialogs and panels. Title text and title inputs support the Copy/Dictionary/Always ignore menu.
- A shared list of 15,981 built-in names, with capitalized matching in rich text, an independent switch, and private account additions. Its index loads once per page and needs no typing-time server requests.
- Native phone long-press selection and Copy, with selection drags taking priority over swipes; desktop right-click menus remain available.
- Bottom Dictionary/Always ignore actions for selected words on phones, including titles and rich editor text. The bar stays above editor Copy/Done and follows the visible viewport; phone selections no longer show the floating app formatting menu. Formatting remains available through Aa.
- A dedicated browser timezone preference that migrates the existing choice and prevents older tabs' unrelated settings saves from overwriting it.
- Ordinary line breaks preserved in the saved view and rich editor. Harmless changes to source escaping or spacing retain rich text editing; unsupported formatting retains the protective source fallback.
- Untitled notes show body text once in Full and Compact views; their first-line preview appears only when folded or in titles-only view, without adding a stored title.
- A one-command release updater and operational checker.

Deployed in v0.7.56: persistent note locks with server-side edit protection, always-accessible unlock indicators, Quick actions/shortcut integration, lock-preserving backups/exports and recovery when another window locks an edited note. Existing notes migrate unlocked. Phone/desktop, migration/restart, ownership/CSRF, transfer and full browser regression tests passed, followed by release CI, live asset/binary/migration/health and backup verification.

Deployed in v0.7.55: configurable keyboard shortcuts with a searchable action catalog, direct selected-text actions, conflict checks and browser/account persistence; wide editor sized to note content and centered vertically; Escape saves/closes with recovery on failure; edit/preview toggle using the Markdown renderer; Tab indentation; adjacent URL and link-edit/navigation fixes; compact Controls and font rollup; Profile/context icons and Cut; improved editor button spacing and subtle scrollbars. Full local browser regression, release CI/restore rehearsal, live binary/assets/health and pre-update backup verification passed.

## Favorites and note display

Deployed in v0.7.43: keyboard-aware bottom navigation and panels; two-column note-action controls with icons; “Show new note box” label and a direct collapse/expand arrow beside “New note”; bounded automatic draft-save retries for temporary failures, clearer permanent errors, and session/version guards. Chromium coverage simulates keyboard viewport shrink/pan, verifies draft recovery, and checks collapse persistence and draft preservation. Real-device verification remains open.

Deployed in v0.7.42: Favorites are hidden from main and Archive by default, with an optional Appearance setting to show them there too. The compact Favorites bar has search and an accessible category dropdown without a visible label. Date-heading diamonds add an entire active/archive date to Favorites across pages; active date sections exclude pinned notes. Screens up to 560 px use persistent bottom navigation with Profile on the right; larger displays retain the header. Controls → Appearance → New note area persists per account in this browser. Paste appears directly below Copy in the right-click menu and works in rich text, source and titles. Phone panels and selection controls clear the bottom navigation. Real-device review remains open.

Deployed in v0.7.40: Titles-view titles and tags use separate rows with a 10 px gap at every width, correcting the overlap left above the mobile cutoff. Only small screens shorten tag labels. The Controls label is now “Height limit” to prevent horizontal scrolling. Browser regression checks cover 320, 600 and 1024 px, including menu overflow.

Deployed in v0.7.39: state-dependent Archive, Search, Recycle and Account/Profile header icons with subtle tinted fills; lid/reflection details distinguish their roles. Titles-view actions use 18 px icons and 36 px circles. Small screens up to 700 px get explicit title/tag rows with a 10 px gap and shortened tag previews (10 name characters plus ellipsis), preserving full names for accessibility, filtering and desktop display.

Deployed in v0.7.38: Favorites from the header and note More menu, with optional placement in quick actions. Tags organize Favorites, including archived notes. Controls → Appearance sets the maximum displayed note text height (160–800 px; default 360 px). Long notes and focused rich/source editors scroll internally. Titles icons follow title text size while retaining their click targets. On screens up to 560 px, tags have a gap below titles and long tag labels use ellipsis without changing stored names.

## Immediate note actions and editor controls

- Desktop mouse/trackpad context menus work alongside touch input; source editors and rich-editor whitespace use the three-item text menu.
- The editor Copy button copies highlighted body/source/title text, or the whole current title and body when no text is selected.
- Done and outside-click close the editor immediately; background saves retain edits on failure. Single-note pin, color, archive/unarchive, and recycle update immediately and roll back failed writes. Bulk moves wait for outstanding edits to save.
- Narrow-phone action rows wrap with their menus aligned inside the viewport; closed Markdown guides do not affect page width.

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

Wasabi was selected on 2026-10-10. A first all-account snapshot is encrypted in a separate `arcomain-backup/gnotes` Restic repository and passed full data checking plus download/hash/database verification. A single private recovery file is available locally outside Git. Automatic Kagoya replication tooling is prepared and locally tested; activation awaits the choice of a dedicated gnotes credential or the existing broader bucket credential. The alert destination remains undecided. See OPERATIONS for setup and recovery.

Admin backup wizard/dashboard deployed in v0.7.50: four-step Wasabi location/permissions/credentials/verified-test setup, with password-protected scheduling/run/stop controls and snapshot history. Bucket and IAM creation remain in the Wasabi administrator console; the app stores only the backup identity and can create an encrypted repository. Activation still requires the user's storage credentials through the wizard. External alerts remain open.

## Mobile verification

1. Retest the current production release (v0.7.56 as of 2026-10-11) with an authenticated account on the Pixel 6a in Brave, Epic, Firefox, and Chrome: long-press and drag selection handles to copy part of a displayed note or title; confirm native selection controls appear. Use the bottom Dictionary/Save word and Always ignore buttons on displayed text, titles, and rich edits, including with the keyboard open; check editor Copy/Done remain accessible. Check personal Names and ignored-word search, Show more, file import, and export downloads. Create and save notes, reopen them in the focused editor, use Copy, the header Archive control, archive removal for one note, a date, and all notes, then recover a removed note from the recycle bin. Check the built-in names switch and personal names, Asia/Tokyo persistence, title context menus, bulk Archive/Recycle icons, outside-click dismissal, spellcheck on/off, active date/Pinned/all removal and recovery, Unhide, tags, ordinary line breaks, swipe actions, note colors, Aa formatting, quick actions, and the code language picker.
2. Check the welcome dialog on a newly created test account without touching existing notes, including Help links and the dismissal choice.
3. Investigate browser-specific failures only when reproduced. Epic previously displayed an empty list because a saved date filter was active; Recent restored the notes.

## Browser and account security

Deployed in v0.7.48: Profile backup/recovery downloads for personal encrypted note snapshots and an admin-only whole-site Wasabi recovery file, with password reauthentication, CSRF, attempt limits and no-store responses. Personal keys are independent of the site Restic key; encrypted import rejects mismatches/tampering and stops if the signed-in session changes during decryption. This does not change the pending whole-site replication schedule or add per-user automatic Wasabi backups.

1. Move inline JavaScript and CSS into versioned static files, then remove `unsafe-inline` from the Content Security Policy.
2. Add user data export and complete self-service account deletion.
3. [x] Configure Brevo recovery in production with verified recovery emails and a private API key. Live admin setup and real inbox reset confirmation remain open.
4. Add administrator MFA and recovery codes.

## Offline access and encryption

Two-stage encryption plan saved on 2026-10-18. Stage one is deployed as v0.8.0-rc.3: the production database and retained local database snapshots are encrypted, and owner-key recovery has been verified. See [ENCRYPTION.md](ENCRYPTION.md) for the implemented format, key provisioning and recovery procedure.

1. **Now: database encryption and encrypted backups for everyone.** Encrypt the live SQLite database, including WAL/journal data, and local server backups; retain encrypted off-server backups. All accounts keep their normal login and workflow. Keep a separate recovery key safely offline with the owner. Before implementation, decide whether the server restarts automatically using an accessible key or the owner supplies the unlock key after each restart. An automatically usable key on the same stolen drive weakens drive-theft protection. An attacker controlling the unlocked server can still read all retained notes. Include existing-data migration, search indexes, temporary files, backup/restore/update tooling, old plaintext backup retention and wrong/missing-key recovery tests in the implementation design.
2. **Later: optional end-to-end encryption for the owner's account.** Encrypt and decrypt notes on the owner's devices, with decryption keys unavailable to the server; other users retain the standard account experience unless they opt in. Design encrypted notes/drafts, client-side search/rendering, device access, passphrase changes, recovery keys and backups together. A login-password reset alone cannot recover encrypted notes; losing all unlocking/recovery material loses access. Plan retirement of older server-readable copies and backups. Keep stage-one encryption in place underneath this feature.

Do not add a separate encrypted-archive workflow. Regular server-side key rotation does not hide past notes while the server retains the ability to decrypt them. Stage 1 is active; the owner selected volatile storage on 2026-10-18. Root setup, RAM-only key provisioning, verified cutover, retained local backup conversion, isolated recovery and missing/wrong-key unlocking checks are complete. At the owner's request, pre-cleanup local/workstation snapshots were subsequently deleted, and a verified post-cleanup encrypted Wasabi snapshot replaces the historical one. Old freed disk blocks/provider snapshots are not securely erased. Keep an additional offline owner key copy. Stage 2 has not started. Simple mode was deployed in v0.8.0-rc.4 on 2026-10-19; see the implementation status below. The owner deleted the three other existing accounts and their notes, retaining `rick` as admin, and enabled new-user signup without invitations; Simple mode should still default on for future regular accounts.

Read-only offline snapshots are deployed in v0.7.45: opt-in device encryption, separate unlock passphrase, manual online refresh, local search/collections, and removal on sign-out. Offline editing and conflict synchronization remain out of scope.

Offline storage must be opt-in and encrypted locally. Private notes should not be copied into IndexedDB or a service-worker cache in plaintext, especially on shared or stolen devices.

End-to-end encryption needs a separate design covering recovery keys, multiple devices, password changes, offline conflict resolution, client-side search, and the inability of an administrator to recover forgotten encrypted data. Server-side SQLite encryption alone does not protect notes from an attacker controlling the running server.

An independently trusted client is required if the threat model includes an actively compromised web server serving malicious JavaScript.

## Scaling decisions

SQLite remains appropriate for the current modest, single-instance deployment. Do not operate multiple writable application instances against independent SQLite databases. Consider PostgreSQL and shared rate limiting only when horizontal scaling becomes necessary.

## Product backlog

Simple mode (deployed in v0.8.0-rc.4 on 2026-10-19): new regular accounts start with writing/autosave, reading/editing, basic formatting, search, delete and recovery. Profile on the far right contains Account, Backup & export, More features and Sign out; Controls initially contains Text size and Light/dark appearance. More features enables organization, writing tools, note controls, appearance, advanced editing and offline reading individually, with a Full mode option and Restore Simple defaults. Save choices per account across devices and apply them consistently to menus, toolbars and shortcuts. Hide controls without removing their stored data; recovery, backup/export and account security stay accessible. Existing accounts retain their current experience; admin `rick` defaults to Full mode with all features and administrator controls available. Verify account defaults/persistence and phone/desktop layouts.

- Favorites collection implemented for v0.7.38, separate from Pin and Archive, including archived favorites, text search and tag categories. A thin diamond uses icy blue fill and small glow strokes when selected. Favorites is optimistic with rollback and account isolation; JSON and ZIP transfers preserve the flag. Browser checks cover narrow/desktop layouts, filtering and failure recovery.

- [x] Export and import notes. Account-menu export supports one, selected, or all notes as readable Markdown or plain text, or versioned gnotes JSON. Multi-note readable exports include a JSON manifest for lossless reimport. Import supports all three formats and the ZIP archive; duplicate handling is explicit. Device review remains open.
- [x] Rich text editing. Selecting text offers bold, italic, and link; typing `/` at the start of a paragraph offers headings, lists, and checklists. Markdown source is available through Account → Advanced. Existing global font and size settings remain available. Device review remains open before expanding formatting tools.
- [x] Add durable note archiving for one note or all active notes. Archived notes leave the main notes view but remain accessible in an Archived view, with individual restore. This is distinct from temporary Hide and from the recycle bin.
- Opt-in encrypted offline reading and editing with explicit synchronization state.
- [x] Interactive task-checkbox toggling from rendered notes.
- [x] Account-specific note tags with filtering and search; deployed in v0.7.23.
- [x] Personal dictionary with offline definitions and an account-specific word list; deployed in v0.7.27.
- Carefully scoped formatting improvements that keep the writing interface uncluttered.

### Distant ideas

- Add editable tables in the rich editor, including row, column, and cell controls, while preserving readable Markdown storage and exports.
- Explore grids after defining whether they should arrange note cards or provide spreadsheet-style cells.
- Explore graphs and charts generated from note data, with a clear source format and safe rendering.
- Explore attaching documents to notes and previewing supported files in place. Define per-account access, file size and type limits, private storage, export behavior, and backup coverage before implementation.

Security requirements and operating procedures remain authoritative in [SECURITY.md](SECURITY.md) and [OPERATIONS.md](OPERATIONS.md).
