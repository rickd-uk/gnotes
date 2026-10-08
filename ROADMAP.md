# gnotes roadmap

Last reviewed: 2026-10-08

## Current production milestone

Production runs at `https://gnotes.rickd.dev` on the Kagoya VPS as a rootless Podman container behind Nginx and TLS, currently on `v0.7.42`. It uses SQLite in WAL mode, checksum-verified releases, health checks, pre-update validated backups, and automatic application rollback during failed updates. The older `hz-sin` systemd deployment is separate from the public site.

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

## Favorites and note display

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

The notification provider and off-server storage destination require an explicit deployment choice before implementation. Wasabi is a possible destination, but no Wasabi backup is currently configured.

## Mobile verification

1. Retest v0.7.37 with an authenticated account on the Pixel 6a in Brave, Epic, Firefox, and Chrome: long-press and drag selection handles to copy part of a displayed note or title; confirm native selection controls appear. Use the bottom Dictionary/Save word and Always ignore buttons on displayed text, titles, and rich edits, including with the keyboard open; check editor Copy/Done remain accessible. Check personal Names and ignored-word search, Show more, file import, and export downloads. Create and save notes, reopen them in the focused editor, use Copy, the header Archive control, archive removal for one note, a date, and all notes, then recover a removed note from the recycle bin. Check the built-in names switch and personal names, Asia/Tokyo persistence, title context menus, bulk Archive/Recycle icons, outside-click dismissal, spellcheck on/off, active date/Pinned/all removal and recovery, Unhide, tags, ordinary line breaks, swipe actions, note colors, Aa formatting, quick actions, and the code language picker.
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
