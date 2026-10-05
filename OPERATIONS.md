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

## Brevo password recovery

Enable transactional email and verify the sender/domain in Brevo. Configure `BREVO_API_KEY`, `GNOTES_EMAIL_FROM`, and `GNOTES_PUBLIC_URL=https://gnotes.rickd.dev` in the private server environment, following `deploy/gnotes.env.example`. Use an API key, not an SMTP key. Never store it in browser assets or Git.

For systemd, use `/etc/gnotes/gnotes.env`. For rootless Podman, pass a private environment file through the container unit with `EnvironmentFile=` (Quadlet) or `--env-file` (podman run). Restart the service after configuration; the container image includes CA certificates for HTTPS to Brevo. A missing API key disables recovery; an invalid configured sender or origin prevents startup.

Sign in, open the account profile, enter a recovery email and current password, then confirm the emailed link. Existing registration emails are not automatically trusted. Test **Forgot password?** with the username and verified email, reset the password, and confirm old sessions and reused links fail. Provider acceptance is tested locally with mocked Brevo responses; real delivery needs a configured key and inbox test. Reset requests are queued in memory, so retry after a minute if a restart interrupted delivery.

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

## Daily backups

Kagoya runs `gnotes-podman-backup.timer` as a user timer at 03:15 server time, with up to 30 minutes of random delay. The job makes an online SQLite snapshot, compresses it, checks integrity and schema with the restore verifier, and then publishes `gnotes-daily-YYYYMMDDTHHMMSSZ.db.gz` in `~/apps/gnotes/backups`. It removes daily archives older than roughly 30 days; release and pre-restore backups are kept separately.

To install or refresh the timer from this checkout:

```bash
scp -F ~/.ssh/config deploy/scripts/backup-gnotes-podman KAG:~/apps/gnotes/bin/
scp -F ~/.ssh/config deploy/systemd/gnotes-podman-backup.service deploy/systemd/gnotes-podman-backup.timer KAG:~/.config/systemd/user/
ssh KAG 'systemctl --user daemon-reload && systemctl --user enable --now gnotes-podman-backup.timer && systemctl --user start gnotes-podman-backup.service'
ssh KAG 'systemctl --user show -P Result gnotes-podman-backup.service && systemctl --user list-timers gnotes-podman-backup.timer --no-pager'
```

Check the newest archive with `restore-gnotes-podman --verify`. A successful timer only proves a local recovery point; copy it to encrypted storage outside Kagoya once the storage destination and credentials are chosen.

The timer was enabled on Kagoya on 2026-09-29. Its first manual run passed the restore verifier and produced a private `0600` archive; the next scheduled run was shown for 2026-09-30 at 03:18 JST.

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

Application rollback does not reverse database migrations. Use the validated pre-update backup only when a database rollback is required. Backup files contain plaintext private notes; keep them restricted and replicate them to encrypted off-server storage.
