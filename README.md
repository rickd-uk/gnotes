# gnotes

A compact, multi-user Markdown notes application written in Go with SQLite and a vanilla browser interface.

See [SECURITY.md](SECURITY.md) for the production checklist, threat boundaries, incident response, and restore procedure.
See [OPERATIONS.md](OPERATIONS.md) for the production inventory, one-command updates, health checks, and rollback procedure.
See [PROGRESS.md](PROGRESS.md) for the current deployed release, completed work, and future plans.
Production runs on a Kagoya VPS as a rootless Podman container with a validated daily backup timer; the Ubuntu/systemd instructions below are an alternative installation layout.
See [ROADMAP.md](ROADMAP.md) for the current production milestone and prioritized future work.

## Current features

- Per-user accounts, sessions, CSRF protection, and note isolation
- Protected `rick` administrator account and signup controls
- Persistent login throttling, escalating cooldowns, invitations, and daily signup caps
- Create, autosave, resume editing by clicking near the desired text position in a note, finish with Done, pin or bulk-unpin, hide, recycle, recover, and permanently delete notes
- Copy a note’s displayed text from its action menu; read-only page text does not select by dragging, while editors remain selectable
- Archive notes for later reading and restore them without deleting them
- Cursor-paginated note and recycle-bin loading with indexed full-text search
- Live title/content search with note and occurrence counts, highlighting, date ranges, scope, and case controls
- Calendar jumps and monthly/weekly archive overviews for large collections
- Server-backed per-user draft autosave and refresh recovery
- Responsive layouts, three note-density modes, and per-user interface-state restoration
- Rich text editing by default, with a visible formatting toolbar for text styles, headings, lists, checklists, quotes, links, code, and dividers. Select text to format it or choose a style before typing; type `/` at the start of a paragraph for block options. Markdown source editing is opt-in under **Account → Advanced**
- Global note font and 65–175% title/text size controls, including in the rich text editor; menu opacity ranges from 0–100%
- User administration with signup policy, login/session details, account disabling, session revocation, and complete deletion
- Account-menu export of all or selected notes and import of gnotes JSON, Markdown, plain text, or a gnotes export ZIP

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

The prioritized plan is maintained in [ROADMAP.md](ROADMAP.md). Database restore tooling covers both systemd and the production Podman deployment. Encrypted off-server backups and operational monitoring follow. Encryption, offline access, account recovery, and horizontal scaling require explicit design decisions documented there before implementation.
