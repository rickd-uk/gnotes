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

The personal dictionary's `saved_words` table is included in the same consistent SQLite snapshots as notes and accounts. The WordNet reference dataset is embedded in the application binary and its license ships in browser assets; it adds no reference data to database backups. Restoring a backup from before v0.7.27 restores the words available at that point (none); startup creates the missing table. See [DICTIONARY.md](DICTIONARY.md) for the pinned source, rebuild procedure, and separate word-list export.

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

## Encrypted Wasabi copy

The first all-account backup was uploaded and restore-verified on 2026-10-10 in a separate Restic repository at `s3:https://s3.ap-northeast-2.wasabisys.com/arcomain-backup/gnotes`. It uses its own encryption password, separate from workstation backups. The recovery file is stored locally at `backups/gnotes-wasabi/gnotes-backup-recovery.env` with mode `0600`; this directory is ignored by Git. Download and keep the file outside Kagoya. It contains the repository address, encryption password and restore instructions, but no Wasabi access credentials. Restoring also requires access to the Wasabi account or a valid access key for this prefix. It unlocks the complete database for **all accounts**, not an individual account export. Never publish the recovery file or include it in the backup repository.

The first copy was made from the workstation using its existing Wasabi profile. Automatic Kagoya replication is **not enabled yet**: the credential choice and alert destination remain pending. The existing Wasabi backup identity was denied IAM policy inspection, so dedicated credentials have not been provisioned. A dedicated user can be created through an administrator's Wasabi console with the policy in `deploy/gnotes-wasabi-policy.json`, granting access only to the `gnotes/` prefix. The alternative is installing the existing broader bucket credential on Kagoya, which requires an explicit user choice.

Profile downloads are separate from automatic replication. For the whole-site admin download, keep a mode `0600` copy of the recovery file in `~/apps/gnotes/backup-recovery` (directory mode `0700`), bind-mount that directory to `/run/gnotes-backup:ro`, and set `GNOTES_SITE_BACKUP_RECOVERY_FILE=/run/gnotes-backup/gnotes-backup-recovery.env`. The file contains the encryption password and restore instructions, not Wasabi access credentials. The app serves it only through the protected POST download endpoint after admin role, CSRF and current-password checks; it is never a static asset. Regular users can download independent personal recovery keys and encrypted note backups, and restore them through Export & import. Those personal snapshots are not uploaded to Wasabi individually. The `backup_keys` table is included in future full database snapshots.

Prepared automation: `deploy/scripts/replicate-gnotes-wasabi`, `deploy/systemd/gnotes-wasabi-backup.service` and its timer. The script selects a published daily archive no older than 36 hours, stages it privately, verifies its database schema/integrity, uploads it with host `kagoya-gnotes` and tag `gnotes`, checks all repository data, restores the latest snapshot to a temporary directory, compares SHA-256 hashes, and verifies the restored database. Any failure exits unsuccessfully for systemd to record. No automatic snapshot deletion or pruning is configured. The proposed daily timer runs at 04:15 server time plus up to 15 minutes, after the existing daily local backup.

Restic 0.18.1, the replication script and both user units are installed on Kagoya as of 2026-10-10; systemd validation passed. The timer remains disabled pending the credential choice. Both available Wasabi profiles lack the IAM permission needed to create the dedicated identity. For activation on another host, install Restic at `~/.local/bin/restic`, the script under `~/apps/gnotes/bin`, and both units under `~/.config/systemd/user`. Create `~/.config/gnotes-backup/wasabi.env` mode `0600` in a mode `0700` directory, containing `RESTIC_REPOSITORY`, `AWS_DEFAULT_REGION=ap-northeast-2`, the chosen `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`, and `RESTIC_PASSWORD_FILE` pointing to a private file containing only the gnotes encryption password. Keep credentials out of unit files and logs. Manually run the service and verify its restore before enabling the timer. Configure alerts separately once their destination is chosen.

To recover on a trusted machine, load the recovery file with `set -a; . ./gnotes-backup-recovery.env; set +a`, clear other password sources with `unset RESTIC_PASSWORD_FILE RESTIC_PASSWORD_COMMAND`, and supply Wasabi credentials securely. Then:

```bash
restic snapshots --host kagoya-gnotes --tag gnotes
restic restore latest --host kagoya-gnotes --tag gnotes --target ./gnotes-restored
deploy/scripts/restore-gnotes-podman --verify ./gnotes-restored/gnotes.db.gz
```

Use a new empty restore directory. This downloads a database archive without changing the running application. Replacing production remains a separate explicit operation through the existing restore procedure. Restic's [repository setup](https://restic.readthedocs.io/en/stable/030_preparing_a_new_repo.html) and [restore documentation](https://restic.readthedocs.io/en/stable/050_restore.html) describe password and Wasabi access requirements.

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
