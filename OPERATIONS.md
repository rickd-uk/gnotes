# gnotes operations

Production runs on the Kagoya VPS through rootless Podman. The SSH alias is `KAG` and uses the `KG-OSK` key. The former `hz-sin` systemd installation is separate and does not serve the public domain.

## Production inventory

| Item | Value |
|---|---|
| SSH host | `KAG` (`133.18.148.95`) |
| Public URL | `https://gnotes.rickd.dev` |
| User service | `systemctl --user gnotes.service` |
| Container | `gnotes`, image `localhost/gnotes:latest` |
| Application files | `/home/rick/apps/gnotes` |
| Database | `/home/rick/apps/gnotes/data/gnotes.db` |
| Backups | `/home/rick/apps/gnotes/backups` |
| Private listener | `127.0.0.1:8081` on the host |
| Reverse proxy | Host Nginx |

Never commit or attach `.env`, databases, backups, private keys, passwords, or session data to a release.

## Release and deploy

Push a clean `main` commit and a new annotated version tag. The GitHub Release workflow runs Go tests, the race test, vet, ShellCheck, restore tests, and a real-server restore rehearsal. Wait for it to publish the Linux archive and `SHA256SUMS`.

Download both release files locally, verify `sha256sum -c SHA256SUMS`, then copy them to a private staging directory on Kagoya together with `update-gnotes-podman`. For example, for `v0.7.2`:

```bash
ssh KAG 'mkdir -p ~/apps/gnotes/.releases/v0.7.2 && chmod 700 ~/apps/gnotes/.releases/v0.7.2'
scp gnotes_v0.7.2_linux_amd64.tar.gz SHA256SUMS deploy/scripts/update-gnotes-podman KAG:~/apps/gnotes/.releases/v0.7.2/
ssh KAG '~/apps/gnotes/.releases/v0.7.2/update-gnotes-podman v0.7.2 /home/rick/apps/gnotes/.releases/v0.7.2'
```

The updater verifies the archive, saves a consistent SQLite backup, builds the image from the release binary and assets, retains the prior image as `localhost/gnotes:rollback-VERSION`, restarts the user service, compares the running binary checksum, and checks local health. It restores the previous image if deployment verification fails. The release also installs the matching Podman update and restore commands under `~/apps/gnotes/bin`.

After deployment:

```bash
ssh KAG 'systemctl --user is-active gnotes.service && podman ps --filter name=gnotes && curl -fsS http://127.0.0.1:8081/api/health'
curl -fsS https://gnotes.rickd.dev/api/health
```

Sign in and inspect notes, drafts, recycle-bin entries, and administrator settings in the browser. The health endpoint checks service and database connectivity, not note completeness.

## Restore

The Kagoya restore command accepts a raw `.db` or compressed `.db.gz` backup. Verification does not stop the service:

```bash
ssh KAG '~/apps/gnotes/bin/restore-gnotes-podman --verify ~/apps/gnotes/backups/BACKUP.db'
```

A real restore replaces the live database with the selected point-in-time backup. It saves the current database first, stops and restarts the user service, and attempts rollback if the restored service is unhealthy:

```bash
ssh KAG '~/apps/gnotes/bin/restore-gnotes-podman --restore ~/apps/gnotes/backups/BACKUP.db'
```

The command can run when the service is already down. A healthy current database is saved as a consistent SQLite backup; a damaged one is preserved with its WAL file. Keep the saved `gnotes-before-restore-*.db` copy until the restored notes and settings have been inspected.

Rehearse restores with an isolated database and service. `deploy/tests/restore-gnotes-podman-test.sh` covers verification, replacement, and rollback; `deploy/tests/restore-gnotes-rehearsal.sh` exercises the real server on a temporary database and port.

## Routine checks and rollback

```bash
ssh KAG 'systemctl --user status gnotes.service --no-pager'
ssh KAG 'podman ps --filter name=gnotes'
ssh KAG 'journalctl --user -u gnotes.service -n 100 --no-pager'
ssh KAG 'python3 -c '\''import sqlite3; print(sqlite3.connect("/home/rick/apps/gnotes/data/gnotes.db").execute("PRAGMA quick_check").fetchone()[0])'\'''
```

The update command retains the previous image. To roll back the application image while keeping the current database:

```bash
ssh KAG 'podman tag localhost/gnotes:rollback-VERSION localhost/gnotes:latest && systemctl --user restart gnotes.service'
```

Application rollback does not reverse database migrations. Use the validated pre-update backup only when a database rollback is required. Backup files contain plaintext private notes; keep them restricted and replicate them to encrypted off-server storage. The Kagoya host currently has no scheduled gnotes backup timer, so add one before relying on automated daily recovery points.
