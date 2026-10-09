# gnotes

A compact, multi-user Markdown notes application written in Go with SQLite and a vanilla browser interface.

See [SECURITY.md](SECURITY.md) for the production checklist, threat boundaries, incident response, and restore procedure.
See [OPERATIONS.md](OPERATIONS.md) for the production inventory, one-command updates, health checks, and rollback procedure.
See [PROGRESS.md](PROGRESS.md) for the current deployed release, completed work, and future plans.
Production runs on a Kagoya VPS as a rootless Podman container with a validated daily backup timer; the Ubuntu/systemd instructions below are an alternative installation layout.
See [ROADMAP.md](ROADMAP.md) for the current production milestone and prioritized future work.

## Current features

Favorites appear only in the Favorites collection by default. Enable **Controls → Appearance → Show Favorites in main and Archive** to include them in those views too. This preference is saved per account in the current browser. Removing the diamond returns a note to its original active or archived view. The diamond beside a date heading adds that entire date section to Favorites, including later pages; active date sections exclude pinned notes. Archive dates use the archive date.

On small screens (up to 560 px), Controls, Search, Favorites, Archive, Recycle Bin, and Profile stay in a larger bottom navigation bar. Tablet and desktop retain the header controls. The small arrow beside **New note** collapses or reopens the writing box without discarding the draft. **Controls → Appearance → Show new note box** controls the same setting. The choice is remembered per account in this browser.

The desktop right-click menu includes Copy, Paste, Dictionary, and Always ignore spelling. Paste works in rich text, Markdown source, and note titles. Clipboard access may require browser permission; keyboard paste and Shift + right-click remain available.

- Per-user accounts, sessions, CSRF protection, and note isolation
- Note tags: open a note’s tools menu → Tags, enter comma-separated labels, and save. Click a tag or choose one in Search to filter; combine tags with text/date searches. Up to 10 tags per note; labels are lowercase, spaces become hyphens, and tags remain with archived/recycled notes. JSON exports and readable ZIP manifests preserve tags
- Protected `rick` administrator account and signup controls
- Persistent login throttling, escalating cooldowns, invitations, and daily signup caps
- Create, autosave, and edit notes in a focused full-screen phone view or centered desktop panel; copy the current text from the lower left or finish with the green check mark and return to the same list position. Pin or bulk-unpin, hide, recycle, recover, and permanently delete notes
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
- Compact Density, Fold/Open, and Tools On/Off controls sit above the notes on the right. **Controls → Appearance → Time zone** defaults to the browser's zone; choose a specific zone such as Japan (Asia/Tokyo) when the browser reports the wrong one. This choice is remembered per account in the current browser and applies to note times, date groups, date search, and archive date actions
- Browser tabs show `gnotes - NOTE_TITLE` while a note is open. Note times omit AM/PM, and displayed note years use two digits
- Code blocks show a clickable language label in the top right; clicking opens the block for editing. Checklists display inline checkboxes without extra bullets
- Space after a bold word or Enter ends bold formatting; spaces inside an existing bold phrase retain it
- Up to three configurable quick actions per note, with the remaining actions in its menu
- Rich text editing by default, with an **Aa** button beside the green check mark that opens formatting controls for text styles, headings, lists, checklists, quotes, links, code, and dividers. Select text to format it or choose a style before typing; type `/` at the start of a paragraph for block options. Markdown source editing is opt-in under **Account → Advanced**
- Global note font and 65–175% title/text size controls, including in the rich text editor; menu opacity ranges from 0–100%
- User administration with signup policy, login/session details, account disabling, session revocation, and complete deletion
- Account-menu export of all or selected notes and import of gnotes JSON, Markdown, plain text, or a gnotes export ZIP

### Offline reading

Choose **Account → Offline reading** while signed in. Set and confirm a separate offline passphrase (at least 12 characters), opt in to device storage, and select **Save offline copy**. The copy includes active and archived notes, Favorites and tags; recycled notes and unfinished drafts are excluded. Notes are shown as text with local search and collection filters. Editing is available online.

After saving, bookmark `/offline.html`. Opening the main site without a connection also opens the offline reader. Enter your offline passphrase to unlock the copy. **Refresh saved copy** replaces it with your current notes while online; it requires the existing offline passphrase. The saved date identifies its age. Changes and deletions made online do not reach an existing copy until you refresh it. A failed refresh preserves the previous copy.

The passphrase cannot be recovered. If forgotten, remove the copy and save a new one while signed in online. **Remove saved copy** removes it from this browser, and signing out also removes it. The reader locks when hidden or after five minutes without activity. Browser storage can be cleared or evicted, so offline copies supplement server backups. Enabling is separate for each browser/device.

### Export and import

Open **Account → Export & import**. Choose gnotes JSON for a complete copy of active, archived, and recycled notes, including dates, pinning, and colors. Plain text exports contain readable note bodies. Markdown export and import appear after enabling **Edit Markdown source** under **Account → Advanced**. When exporting multiple notes in a readable format, gnotes creates a ZIP with a JSON manifest that preserves note details for reimport. Keep the manifest with the files; the importer reads it as the source of record.

Select notes in the panel to export one or more, or use **Export all**. Imports add notes to the signed-in account. **Skip exact matches** avoids adding a note already present with the same details; **Create another copy** imports it again. A standalone Markdown or text file becomes one active note named after the file. Imports are checked before any note is written and are limited to 64 MB and 10,000 notes per file.

The editor stores a Markdown representation of rich text so existing notes, search, and exports continue to work. Enter creates a visible paragraph break; Shift+Enter creates a line break. Notes containing Markdown that cannot be converted back exactly open in source editing to preserve their original text. The Markdown source preference is saved per account in the current browser.

## Local development

Requirements: Go 1.25 or later. Node.js is needed only to rebuild the bundled rich text editor.

```bash
make run
```

After changing [web/rich-editor.js](web/rich-editor.js), rebuild the checked-in browser bundle with `npm ci && npm run build:editor`.

The server listens on `http://127.0.0.1:8080` by default. The first account must be named `rick`; it becomes the administrator and claims notes created before accounts were introduced.

Supported environment variables:

| Variable | Default | Purpose |
|---|---|---|
| `HOST` | `127.0.0.1` | HTTP bind address |
| `PORT` | `8080` | HTTP port |
| `DATABASE_PATH` | `gnotes.db` | SQLite database location |
| `BACKUP_DIRECTORY` | `/var/backups/gnotes` | Destination used by the backup script |
| `GNOTES_SECURE_COOKIES` | `false` | Set to `true` behind production HTTPS |
| `GNOTES_SETUP_TOKEN` | none | Required for first-admin setup when accessed remotely |
| `BREVO_API_KEY` | none | Server-only Brevo API key; enables email recovery when configured |
| `GNOTES_EMAIL_FROM` | none | Verified sender email address in Brevo |
| `GNOTES_EMAIL_FROM_NAME` | `gnotes` | Sender display name |
| `GNOTES_PUBLIC_URL` | none | Application origin for email links; HTTPS required except on localhost |

gnotes JSON exports now use format version 2; version 1 remains importable. A tagged single-note Markdown/text export uses a ZIP with a metadata manifest so tags survive reimport.

See [OPERATIONS.md](OPERATIONS.md#brevo-password-recovery) for email setup. Existing account emails require verification before they can recover a password.

The personal [dictionary](DICTIONARY.md) provides offline English definitions and private saved-word lists. Right-click text for Copy, Dictionary, or Always ignore spelling; select a word, or use Account → Dictionary. Controls → Spellcheck provides an on/off setting and separate account-stored ignored-word and Names lists for people, companies, and places. Both lists are available across devices and included in database backups; older browser lists migrate automatically on sign-in or reload. WordNet 3.1 is bundled under its included license; lookup makes no external requests.

Run validation with:

```bash
go test ./...
go test -race ./cmd/server
go vet ./...
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
go build -trimpath -ldflags="-s -w" -o gnotes ./cmd/server
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
