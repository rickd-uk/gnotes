# gnotes

A compact, multi-user Markdown notes application written in Go with SQLite and a vanilla browser interface.

See [SECURITY.md](SECURITY.md) for the production checklist, threat boundaries, incident response, and restore procedure.
See [OPERATIONS.md](OPERATIONS.md) for the production inventory, one-command updates, health checks, and rollback procedure.
See [PROGRESS.md](PROGRESS.md) for the current deployed release, completed work, and future plans.
Production runs on a Kagoya VPS as a rootless Podman container with a validated daily backup timer; the Ubuntu/systemd instructions below are an alternative installation layout.
See [ROADMAP.md](ROADMAP.md) for the current production milestone and prioritized future work.
See [ENCRYPTION.md](ENCRYPTION.md) for deployment-wide database encryption, key ownership, migration and recovery. Kagoya production uses SQLCipher for the live database and retained local database backups as of v0.8.0-rc.3, with an owner-supplied RAM-only server key. Other installations require explicit activation.

## Current features

New regular accounts start in **Simple mode**: writing/autosave, reading/editing,
formatting, search, delete/recovery, and account security. **Profile → More features**
enables Organization, Writing tools, Note controls, Appearance, Advanced editing,
and Offline reading separately, or switches to Full mode. Restore Simple defaults
hides those tools while preserving notes and their metadata. These choices and
light/dark appearance are saved per account on the server; other devices read them
on sign-in/reload or when returning to the tab. Existing accounts, including `rick`,
retain Full mode. Basic text size and light/dark appearance stay in Controls.

**Profile → Backup & recovery** opens personal encrypted backup/key downloads,
export/import, and a browser-only personal recovery test. Administrators also see
local database backup history/scheduling, Wasabi status, database-key availability,
and dated recovery-test evidence. Password-confirmed controls create local backups,
enable/pause either schedule, and test the latest local or off-server database
backup in isolated temporary files. Off-server tests download a snapshot. Server
recovery tests use the server key; test an offline key copy separately with
`gnotes db recovery-test` as described in [ENCRYPTION.md](ENCRYPTION.md).


**Profile → Keyboard shortcuts** lists navigation, note actions, editing, formatting, selected-text Cut/Copy/Paste/Dictionary/Ignore, display, search, account, spelling, import/export, offline reading and administration controls. Search for an action and select **Set keys** to record a combination; conflicts are rejected. Clear individual bindings or reset defaults. Assignments are stored per account in this browser. Note commands target the focused or pointed-at note; unavailable commands are ignored. Defaults: `/` finds notes, Ctrl+Enter saves and closes, and Ctrl+Shift+P toggles preview. Standard text editing, Tab indentation, Escape and browser keys retain their roles. Range and selection controls receive keyboard focus so you can adjust them with arrow keys.

Use the **crossed-out pencil** in note tools (⋯) to make a note read-only. The icon stays visible without a text label; click it to enable editing. Read-only mode prevents changes to title, text, checklist items, tags and color on the server, across devices. Add Read only to quick actions under Controls or assign **Toggle read-only mode** in Keyboard shortcuts. This is an editing control, not encryption. Locks survive archiving, recycling/recovery, JSON/encrypted backups and ZIP exports. If another window locks a note while you edit it, rejected edits remain available to save after unlocking.

Favorites appear only in the Favorites collection by default. Enable **Controls → Display → Favorites** to include them in those views too. This preference is saved per account in the current browser. Removing the diamond returns a note to its original active or archived view. The diamond beside a date heading adds that entire date section to Favorites, including later pages; active date sections exclude pinned notes. Archive dates use the archive date.

On small screens (up to 560 px), Controls, Search, Favorites, Archive, Recycle Bin, and Profile stay in a larger bottom navigation bar. Tablet and desktop retain the header controls. The small arrow beside **New note** collapses or reopens the writing box without discarding the draft. **Controls → Display → New note box** controls the same setting. The choice is remembered per account in this browser.

Editing fields use the browser’s native right-click menu for direct Cut, Copy and Paste, without the app requesting clipboard access. Displayed notes retain the app’s Copy, Dictionary and Always ignore spelling menu. Dictionary and spelling actions remain available through selection controls and configurable shortcuts; Shift + right-click opens the browser menu on displayed notes.

The **N** button beside the title switches new and existing notes to browser Vim editing of their Markdown text. Press `i` for Insert mode and Escape for Normal mode; motions, visual selection, search, undo/redo and other Vim commands come from [CodeMirror's Vim bindings](https://github.com/replit/codemirror-vim). `:w` saves without closing (a draft for new notes); `:wq`, `:x` and Ctrl/Command+Enter save and close/finalize. Autosave remains active, so `:q` also keeps changes and closes. Existing-note preview remains available. Switch N off to return to the normal editor; unsupported Markdown retains the source fallback. The preference is per account in this browser, and the separate editor bundle loads only when Vim is requested. Signing out clears Vim registers, macros/search history and custom mappings. Vim commands take priority over custom app shortcuts inside its editor, except save, preview and the Vim toggle.

The header Profile menu uses softly colored icons to identify actions. Preview, formatting and save controls have distinct colors and wider spacing; formatting has a larger **Aa** button. While editing a note, use the eye icon to preview the current Markdown, including unsaved text and tables; the pencil returns to editing. Notes always open in edit mode. Short notes keep the wide editor but use only the height they need, centered on screen; long notes grow to the available height and scroll. Escape saves and closes the note; an unsuccessful save reopens the editor with the text retained. Nested dialogs close first. Preview uses the server renderer and requires a connection. Tab indents selected lines, Shift+Tab outdents, and lists use these keys for nesting. Ordinary paragraph indentation in the normal editor stays as text through save, preview and reopening, including leading spaces typed manually. Older notes and raw Markdown/Vim text indented with tabs or spaces also display as ordinary literal text, retaining indentation and line breaks. Explicit fenced code remains code. For an existing fenced block that should be ordinary text, click inside it and choose **Normal text**, then save. Choose **Aa → { } Code block** or **/ → Code block** for code. Source-mode Tab also preserves normal text indentation while retaining literal spaces inside fenced code and for list nesting. Saved links open normally; Shift-click opens their note for editing. In the rich editor, click a link to place the cursor, Ctrl/Command-click to open it, or choose **Aa → Link** to change or open its address. Horizontal scrollbars in notes and editors are thin and muted.

The rarely used **Delete all notes** action is under **Account → Manage notes**. Its confirmation dialog still lets you cancel before moving active notes to the recycle bin.

- Per-user accounts, sessions, CSRF protection, and note isolation
- Note tags: open a note’s tools menu → Tags, enter comma-separated labels, and save. Click a tag or choose one in Search to filter; combine tags with text/date searches. Up to 10 tags per note; labels are lowercase, spaces become hyphens, and tags remain with archived/recycled notes. JSON exports and readable ZIP manifests preserve tags
- Protected `rick` administrator account and signup controls
- Persistent login throttling, escalating cooldowns, invitations, and daily signup caps
- Create, autosave, and edit notes in a focused phone view or desktop panel with 16 px screen margins; copy the current text from the lower left or finish with the green check mark and return to the same list position. Pin or bulk-unpin, hide, recycle, recover, and permanently delete notes
- Select note titles and text by dragging, then copy normally; selection does not open the editor. Copy whole notes from their action menu
- Phone long presses keep native selection handles and Copy; desktop right-clicks retain Copy/Dictionary/Always ignore
- Selected words on phones expose Dictionary and Always ignore in a bottom bar, above editor Copy/Done controls; Dictionary includes Save word
- Spellcheck includes a shared list of 15,981 built-in names, with an independent switch and private personal additions. Capitalized name matching runs locally while typing
- Search personal Names and ignored words, show larger lists in batches of 50, export both lists as JSON, and merge JSON/text imports without replacing existing entries or name types
- Archive notes for later reading and restore them without deleting them; move one archived note, all notes archived on a displayed date, or the full archive to the recycle bin
- Move an active date section, all pinned notes, or all active notes to the recycle bin after confirmation; date sections exclude pinned notes, and removal includes hidden notes and later pages
- Icon buttons below active notes archive or recycle the whole collection after confirmation. Cancel is a bordered button; clicking outside a modal dismisses it without confirming its action
- Untitled notes show their body once in Full and Compact views; a first-line preview appears only when folded or in titles-only view, without adding a stored title
- Cursor-paginated note and recycle-bin loading with indexed full-text search
- Live title/content search with note and occurrence counts, highlighting, date ranges, scope, and case controls
- Calendar jumps and monthly/weekly archive overviews for large collections
- Server-backed per-user draft autosave and refresh recovery
- Optional Brevo password recovery: verify an email from your profile, then use **Forgot password?** to receive an expiring reset link. A reset signs out existing sessions
- Responsive layouts, three note-density modes, and per-user interface-state restoration. A small chevron beside each note timestamp rolls up that note's body without hiding its title or actions; its state is remembered per account in this browser
- Compact Density, Fold/Open, and Tools On/Off controls sit above the notes on the right. **Controls → Display → Time zone** defaults to the browser's zone; choose a specific zone such as Japan (Asia/Tokyo) when the browser reports the wrong one. This choice is remembered per account in the current browser and applies to note times, date groups, date search, and archive date actions
- Browser tabs show `gnotes - NOTE_TITLE` while a note is open. Note times omit AM/PM, and displayed note years use two digits
- Code blocks show a clickable language label in the top right; clicking opens the block for editing. Checklists display inline checkboxes without extra bullets
- Space after a bold word or Enter ends bold formatting; spaces inside an existing bold phrase retain it
- Up to three configurable quick actions per note, with the remaining actions in its menu
- Rich text editing by default, with an **Aa** button beside the green check mark that opens formatting controls for text styles, headings, lists, checklists, quotes, links, code, and dividers. Select text to format it or choose a style before typing; type `/` at the start of a paragraph for block options. Markdown source editing is opt-in under **Account → Advanced**
- Compact two-column Display settings and a collapsed **Fonts & text** section for fonts, 65–175% title/text sizes, and note height; menu opacity ranges from 0–100%
- User administration with signup policy, login/session details, account disabling, session revocation, and complete deletion
- Account-menu export of all or selected notes and import of gnotes JSON, Markdown, plain text, or a gnotes export ZIP

### Backup recovery keys

Open **Account → Profile → Backup & recovery** and enter your current password before each download.

- **Download my recovery key** saves a private key file for your personal encrypted note backups. The key stays the same when downloaded again, including from another device.
- **Download my encrypted backup** saves your active, archived and recycled notes, including dates, tags, Favorites, pinning and colors. Drafts, dictionaries, account settings and login data are excluded. Keep this backup and its key in separate safe locations.
- Admins also have **Download whole-site recovery key**, which unlocks the all-account database backup in Wasabi. Wasabi account access or valid storage credentials are required separately. Regular users cannot retrieve this key.

To restore a personal backup, open **Account → Export & import**, choose the `.gnotes-backup` file, choose its recovery key when prompted, and select **Import file**. Restoration adds notes to the account currently signed in, with the existing duplicate-choice controls. A wrong key or damaged file is rejected before any notes are imported. Personal downloads are made on demand; they are separate from the whole-site Wasabi backup schedule. These keys do not reset your login password or unlock offline reading.

Personal backups are encrypted on the server with an account-specific key retained in the database. This protects a downloaded backup without its key; it does not make the live server end-to-end encrypted. An account's complete deletion removes its retained key, so keep downloaded keys if you want to restore old backups later.

### Whole-site backup setup and controls

Admins can open **Account → Profile → Whole-site backup dashboard → Set up or change Wasabi storage**. The four steps collect the bucket/region/folder, generate a folder-scoped Wasabi policy, accept a backup user's access credentials, and test a fresh upload plus verified restore before saving. Create the bucket and programmatic user in the Wasabi console with an administrator account; the wizard creates the encrypted repository when explicitly allowed. The established site encryption key is retained when changing locations. Keep older recovery files for older locations.

After a successful test, download the updated whole-site recovery file using **Backup & recovery**, save it outside Kagoya, then confirm this and enable the daily schedule. The dashboard shows the next run, last service result, setup verification time and recent snapshot history. Controls run a fresh backup, pause/resume scheduling, stop a current backup or refresh history. Every action requires the current gnotes password. Pausing/stopping never deletes backups. Storage credentials are stored outside the app container and are excluded from recovery downloads. The host control service must be installed; see OPERATIONS.

### Offline reading

Choose **Account → Offline reading** while signed in. Set and confirm a separate offline passphrase (at least 12 characters), opt in to device storage, and select **Save offline copy**. The copy includes active and archived notes, Favorites and tags; recycled notes and unfinished drafts are excluded. Notes are shown as text with local search and collection filters. Editing is available online.

After saving, bookmark `/offline.html`. Opening the main site without a connection also opens the offline reader. Enter your offline passphrase to unlock the copy. **Refresh saved copy** replaces it with your current notes while online; it requires the existing offline passphrase. The saved date identifies its age. Changes and deletions made online do not reach an existing copy until you refresh it. A failed refresh preserves the previous copy.

The passphrase cannot be recovered. If forgotten, remove the copy and save a new one while signed in online. **Remove saved copy** removes it from this browser, and signing out also removes it. The reader locks when hidden or after five minutes without activity. Browser storage can be cleared or evicted, so offline copies supplement server backups. Enabling is separate for each browser/device.

### Export and import

Open **Account → Export & import**. Choose gnotes JSON for a complete copy of active, archived, and recycled notes, including dates, pinning, and colors. Plain text exports contain readable note bodies. Markdown export and import appear after enabling **Edit Markdown source** under **Account → Advanced**. When exporting multiple notes in a readable format, gnotes creates a ZIP with a JSON manifest that preserves note details for reimport. Keep the manifest with the files; the importer reads it as the source of record.

Select notes in the panel to export one or more, or use **Export all**. Imports add notes to the signed-in account. **Skip exact matches** avoids adding a note already present with the same details; **Create another copy** imports it again. A standalone Markdown or text file becomes one active note named after the file. Imports are checked before any note is written and are limited to 64 MB and 10,000 notes per file.

The editor stores a Markdown representation of rich text so existing notes, search, and exports continue to work. Enter creates a visible paragraph break; Shift+Enter creates a line break. Notes containing Markdown that cannot be converted back exactly open in source editing to preserve their original text. The Markdown source preference is saved per account in the current browser.

## Local development

Requirements: Go 1.25 or later, a C compiler and OpenSSL development headers/libraries for SQLCipher. Node.js is needed to rebuild or validate the bundled rich text editor. Use the Makefile to supply the native encryption and FTS5 build flags. For direct Go commands in this document, first export `GOFLAGS=-tags=sqlite_fts5` and `CGO_CFLAGS='-O2 -g -ftls-model=initial-exec'`. Release binaries require compatible glibc and OpenSSL 3; see ENCRYPTION.md.

```bash
make run
```

After changing [web/rich-editor.js](web/rich-editor.js), its [spelling module](web/spelling.mjs), or [web/vim-editor.js](web/vim-editor.js), rebuild the checked-in browser bundles with `npm ci && npm run build:editor`. Run `npm run test:editor` for incremental spelling regressions and `npm run bench:editor` for the synthetic 500-paragraph spelling benchmark. The benchmark measures spelling transactions only, excluding layout, Markdown serialization and network saves.

[public/note-list.js](public/note-list.js) reconciles note cards by ID, keeping unchanged cards and open editors attached. Card HTML generation remains in `index.html`. Closed cards contain no editor fields or controls: `openNoteEditor()` creates them with current note data and preferences, and closing removes them after capturing edits. Use `currentEdit()` only for the active note; absent controls return `null`. Browser regressions include 500-note timings/element counts, selection/editor preservation, paging, pinning, search highlighting, and repeated rich/source editor creation with current spelling/toolbar settings and Markdown fallback. With Chromium and ChromeDriver running on port 19515, run `GNOTES_BROWSER_CHECK=1 go test ./cmd/server -run TestIncrementalNoteRenderingBrowser -count=1 -v` for the focused check, or `GNOTES_BROWSER_CHECK=1 go test ./...` for the complete browser suite.

App rich editors use `onDirty` callbacks to schedule autosave without serializing Markdown on every ordinary text edit. `currentDraft()` and `currentEdit()` synchronize the source buffers when text is needed; mode changes also flush before destroying the rich editor. The shared `mountRichInput()` binding preserves explicit source-buffer edits made after the last rich edit. The adapter caches serialized Markdown by immutable document identity. Use these read helpers when adding save/preview actions instead of reading a hidden source buffer directly. Run `GNOTES_BROWSER_CHECK=1 go test ./cmd/server -run 'Test(EditorSerializationCostBrowser|DeferredEditorReadsBrowser)' -count=1 -v` for the isolated edit-cost benchmark and immediate-save, preview, mode-switch and page-exit regressions. The benchmark uses hidden DOM with spelling disabled and excludes network requests and visible layout.

The Vim build generates [dependency notices](public/vim-editor.LICENSE.txt) alongside its bundle. The rich and Vim adapters share `mountBoundEditor()` for cached, deferred source reads and explicit source-buffer ownership. Vim Ex handlers dispatch to their invoking editor and account; Escape remains inside Vim. The loader checks account/request ownership before switching modes, supports retry and rapid cancellation, and shares a single same-origin bundle load. Run `GNOTES_BROWSER_CHECK=1 go test ./cmd/server -run 'TestVim(Editor|LoadLifecycle)Browser' -count=1 -v` for real-keyboard phone/desktop checks, save/recovery, previews, lossless switching, page-exit capture, lazy-load failure/retry and account isolation.

Note loads use request-scoped cancellation and ownership checks. Typing invalidates the old request immediately and retains the 250 ms search debounce; only the latest load can publish results, cursor, errors or loading state. Restored/revealed notes stage into the same request-local map, and pagination cursors are bound to their original filters. Request contexts reach authentication and paged-note/search SQL reads and rendered-cache writes. SQLite safety pragmas apply on every connection, including replacements after cancellation. Run `GNOTES_BROWSER_CHECK=1 go test ./cmd/server -run TestLatestSearchBrowser -count=1 -v` for delayed-response phone/desktop regressions. Backend checks are `go test ./cmd/server -run 'Test(PagedReadsCancel|Canceled)'` and `go test ./internal/db -run TestConnectionPragmasSurviveReconnect`.

The server listens on `http://127.0.0.1:8080` by default. The first account must be named `rick`; it becomes the administrator and claims notes created before accounts were introduced.

Supported environment variables:

| Variable | Default | Purpose |
|---|---|---|
| `HOST` | `127.0.0.1` | HTTP bind address |
| `PORT` | `8080` | HTTP port |
| `DATABASE_PATH` | `gnotes.db` | SQLite database location |
| `GNOTES_DATABASE_KEY_FILE` | none | Private generated 256-bit hex key file; enables SQLCipher for the whole database |
| `GNOTES_REQUIRE_ENCRYPTION` | unset | Set to `1` after migration to reject startup without a database key |
| `GNOTES_VOLATILE_KEY` | unset | Set to `1` for owner-provisioned RAM-only keys; disables core dumps and rejects disk-backed keys or active swap |
| `BACKUP_DIRECTORY` | `/var/backups/gnotes` | Destination used by the backup script |
| `GNOTES_SECURE_COOKIES` | `false` | Set to `true` behind production HTTPS |
| `GNOTES_SETUP_TOKEN` | none | Required for first-admin setup when accessed remotely |
| `BREVO_API_KEY` | none | Server-only Brevo API key; enables email recovery when configured |
| `GNOTES_EMAIL_FROM` | none | Verified sender email address in Brevo |
| `GNOTES_EMAIL_FROM_NAME` | `gnotes` | Sender display name |
| `GNOTES_PUBLIC_URL` | none | Application origin for email links; HTTPS required except on localhost |
| `GNOTES_SITE_BACKUP_RECOVERY_FILE` | none | Private recovery file offered only to reauthenticated admins; mount it read only outside public assets |

gnotes JSON exports use format version 4 and preserve note locks; versions 1–3 remain importable. A tagged or locked single-note Markdown/text export uses a ZIP with a metadata manifest so its state survives reimport.

See [OPERATIONS.md](OPERATIONS.md#brevo-password-recovery) for email setup. Existing account emails require verification before they can recover a password.

The personal [dictionary](DICTIONARY.md) provides offline English definitions and private saved-word lists. Right-click text for Copy, Dictionary, or Always ignore spelling; select a word, or use Account → Dictionary. Controls → Spellcheck provides an on/off setting and separate account-stored ignored-word and Names lists for people, companies, and places. Both lists are available across devices and included in database backups; older browser lists migrate automatically on sign-in or reload. WordNet 3.1 is bundled under its included license; lookup makes no external requests.

Run validation with:

```bash
make check
make test-encryption
```

### Generate test notes

To exercise pagination, search, date sections, pinned notes, and the recycle bin in a local database, generate 500 notes for `rick`:

```bash
make seed
```

Choose a larger count, another local database, or another account with Make variables:

```bash
make seed SEED_COUNT=5000
make seed SEED_DATABASE=/tmp/gnotes-test.db SEED_USER=alice SEED_COUNT=1000
```

The generator includes varied Markdown, untitled and long notes, dates from today through more than two years ago, mixed-case `OrchidSignal` search samples, pinned notes, and notes in the recycle bin. It refuses the production `/var/lib/gnotes` path unless its explicit production override is used.

Remove only notes created by the generator with:

```bash
make seed-clean
```

## Ubuntu and systemd deployment

This layout keeps the application private on localhost and exposes it only through an HTTPS reverse proxy:

```text
/opt/gnotes/                 binary, public assets, and operational scripts
/var/lib/gnotes/gnotes.db    persistent SQLite database
/etc/gnotes/gnotes.env       protected configuration
/var/backups/gnotes/         local validated backups
```

### 1. Build

Build on the server or build a Linux binary on another compatible machine:

```bash
make build
```

### 2. Create the service account and directories

```bash
sudo useradd --system --home-dir /var/lib/gnotes --shell /usr/sbin/nologin gnotes
sudo install -d -o root -g root -m 0755 /opt/gnotes /opt/gnotes/bin
sudo install -d -o gnotes -g gnotes -m 0700 /var/lib/gnotes /var/backups/gnotes
sudo install -d -o root -g gnotes -m 0750 /etc/gnotes
```

### 3. Install the application

From the repository checkout:

```bash
sudo install -o root -g root -m 0755 gnotes /opt/gnotes/gnotes
sudo cp -a public /opt/gnotes/public
sudo chown -R root:root /opt/gnotes/public
sudo find /opt/gnotes/public -type d -exec chmod 0755 {} \;
sudo find /opt/gnotes/public -type f -exec chmod 0644 {} \;
sudo install -o root -g root -m 0755 deploy/scripts/backup-gnotes /opt/gnotes/bin/backup-gnotes
sudo install -o root -g root -m 0755 deploy/scripts/update-gnotes /opt/gnotes/bin/update-gnotes
sudo install -o root -g root -m 0755 deploy/scripts/check-gnotes /opt/gnotes/bin/check-gnotes
sudo install -o root -g root -m 0755 deploy/scripts/restore-gnotes /opt/gnotes/bin/restore-gnotes
```

The service uses `/opt/gnotes` as its working directory, so the `public` directory must be installed there with the binary.

### 4. Configure secrets and storage

```bash
sudo cp deploy/gnotes.env.example /etc/gnotes/gnotes.env
sudo chown root:gnotes /etc/gnotes/gnotes.env
sudo chmod 0640 /etc/gnotes/gnotes.env
openssl rand -hex 32
```

Put the generated value in `GNOTES_SETUP_TOKEN`. Keep `HOST=127.0.0.1`, set `GNOTES_SECURE_COOKIES=true`, and keep the database under `/var/lib/gnotes`.

After the first administrator has been created and verified, remove `GNOTES_SETUP_TOKEN` from the environment file and restart the service. A transferred database that already contains the administrator does not need a setup token.

For an existing installation, stop gnotes and transfer a SQLite `.backup` into place instead of copying a database while it is being written:

```bash
sqlite3 gnotes.db ".backup '/tmp/gnotes-transfer.db'"
scp /tmp/gnotes-transfer.db your-server:/tmp/gnotes-transfer.db
sudo install -o gnotes -g gnotes -m 0600 /tmp/gnotes-transfer.db /var/lib/gnotes/gnotes.db
```

### 5. Install and start systemd units

```bash
sudo install -o root -g root -m 0644 deploy/systemd/gnotes.service /etc/systemd/system/gnotes.service
sudo install -o root -g root -m 0644 deploy/systemd/gnotes-backup.service /etc/systemd/system/gnotes-backup.service
sudo install -o root -g root -m 0644 deploy/systemd/gnotes-backup.timer /etc/systemd/system/gnotes-backup.timer
sudo systemctl daemon-reload
sudo systemctl enable --now gnotes.service gnotes-backup.timer
sudo systemctl status gnotes.service
```

The service runs as the unprivileged `gnotes` user with a read-only system view and write access only to `/var/lib/gnotes`.

Before starting the units, confirm the server provides `sqlite3` and `gzip`, which are used by the backup job.

### 6. Configure Nginx and HTTPS

Copy the Nginx server, proxy, and rate-limit snippets; replace every occurrence of `notes.example.com`, and point its certificate paths at the real certificate:

```bash
sudo install -o root -g root -m 0644 deploy/nginx/gnotes.conf /etc/nginx/sites-available/gnotes
sudo install -o root -g root -m 0644 deploy/nginx/gnotes-proxy.conf /etc/nginx/snippets/gnotes-proxy.conf
sudo install -o root -g root -m 0644 deploy/nginx/gnotes-rate-limits.conf /etc/nginx/conf.d/gnotes-rate-limits.conf
sudo ln -s /etc/nginx/sites-available/gnotes /etc/nginx/sites-enabled/gnotes
sudo nginx -t
sudo systemctl reload nginx
```

Only ports 80 and 443 should be public. Port 8080 remains bound to localhost. Verify deployment with:

```bash
curl -fsS https://notes.example.com/api/health
journalctl -u gnotes.service -n 100 --no-pager
```

### 7. Backups

The included timer makes an online SQLite backup each day, verifies it, compresses it, and retains 30 days locally. Verify an archive or restore it with the commands in [OPERATIONS.md](OPERATIONS.md).

```bash
sudo systemctl start gnotes-backup.service
sudo systemctl status gnotes-backup.service
sudo systemctl list-timers gnotes-backup.timer
```

Local backups do not protect against total server loss. Replicate `/var/backups/gnotes` to separate encrypted storage with a tool such as Restic or Borg, and test restoration periodically.

### Updating

Tagged releases are tested and packaged by GitHub Actions. After publishing a version tag, update the server with:

```bash
sudo /opt/gnotes/bin/update-gnotes v0.2.0
```

The updater downloads the requested versioned release, verifies its SHA-256 checksum, runs a validated database backup, installs the binary and browser assets together, checks health, and automatically attempts application rollback on failure. See [OPERATIONS.md](OPERATIONS.md) for release creation, complete checks, and manual rollback.

## Security and production roadmap

The prioritized plan is maintained in [ROADMAP.md](ROADMAP.md). Database restore tooling covers both systemd and the production Podman deployment. Encrypted off-server backups and operational monitoring follow. Encryption, offline access, and horizontal scaling require explicit design decisions documented there before implementation.
