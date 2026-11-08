# GNotes: check, recover and protect your key

For downloading and restoring your notes too, start with [BACKUP-GUIDE.md](BACKUP-GUIDE.md).

These commands run in a terminal **on your own workstation**, not inside an SSH
session on the server. Type `exit` first if your terminal is connected to `KAG`.
They need Python 3, SSH, your existing `KAG` SSH connection and your existing key.

## If the site shows Offline reading

Run:

```sh
gnotes-check
```

This checks the public website, SSH access, the server's last startup time,
the app service, whether its key is in server memory, and the app's internal
health check. It changes nothing. An unhealthy website returns exit status 1.

| Result | What to do |
|---|---|
| Website healthy | Open <https://gnotes.rickd.dev/> again. Reload the main URL if Offline reading remains visible. Preserve any unsaved editor text before reloading. |
| SSH access failed | Run `ssh-add`, then `ssh KAG`. Unlock your usual SSH key when asked. Once connected, type `exit` and retry `gnotes-check`. If SSH still fails, check your Internet connection and the host provider's console. |
| Server key missing | Run `gnotes-recover`. This is expected after a host reboot. |
| Key present, app unavailable | Inspect service status and logs using the commands below. |
| Internal health good, public site unavailable | Check the host reverse proxy/network/DNS. Supplying the key again will not fix that. |

## Bring GNotes back after a reboot

```sh
gnotes-recover
```

It checks whether the site is already online, checks SSH and server state,
supplies your **existing** key through encrypted SSH stdin when missing, and
checks public health afterward. The existing server helper verifies the key
against the database before starting GNotes. The command does not generate a key,
restore an old database, deploy an update, or change the boot/security setup.
The server-side verification can take several minutes; let it finish.

Success says **GNotes is online and its database is connected**. Open the main
URL again. The health check confirms database connectivity, not a full restore
test or the completeness of every note.

Why this happens: the host stores notes encrypted on disk but keeps the key
only in memory. Host restarts clear memory. The app waits for your key; meanwhile
your browser can show its saved Offline reading page. App restarts without a
host reboot normally keep working because the key remains in host memory.

## Back up the key now

The original is on this workstation at:

```text
/home/rick/.config/gnotes/recovery/database-v1.key
```

**Do not lose this file. Your account password cannot replace it.** The personal
backup key downloaded in Profile and the Wasabi recovery password are different
keys; neither replaces this whole-database key.

1. Connect a trusted USB drive or external disk, preferably encrypted.
2. Find its mounted folder in your file manager. Replace the example path below
   with that real folder; keep quotation marks if it contains spaces.
3. Run:

   ```sh
   gnotes-backup-key "/run/media/rick/YOUR-DRIVE"
   ```

The command creates a private `gnotes-recovery` folder there, copies
`database-v1.key` with private permissions, reads it back and compares it with
the original. An existing matching copy is verified; a different key is never
overwritten. No key contents are printed. Git repository destinations are
rejected. Storage must support private Unix permissions; FAT/exFAT drives may
need an encrypted Linux filesystem/container instead.

Safely eject and disconnect the drive. Keep it secure, separate from both your
workstation and database backups. A second folder on the same workstation is
not protection against losing that workstation. Keep a copy of this guide and
the workstation helper on the recovery drive too; neither contains a key.

You need **both** an encrypted database backup and its matching database key to
recover notes after losing the server. This command backs up only the key. A
read-back comparison verifies the copied file; an isolated database recovery
test is the next level of validation—see [ENCRYPTION.md](ENCRYPTION.md).

## If recovery fails

Read-only commands to inspect the app:

```sh
ssh KAG 'systemctl --user status gnotes.service --no-pager'
ssh KAG 'journalctl --user -u gnotes.service -n 40 --no-pager'
```

| Message | Meaning / next step |
|---|---|
| `ConditionPathExists=/run/gnotes/keys/database.key` unmet | Key cleared after reboot. Run `gnotes-recover` on the workstation. |
| `Permission denied (publickey)` | SSH authentication needs fixing first. Run `ssh-add`, then test `ssh KAG`. |
| SSH connection refused or timed out | Check Internet access and host/provider status. Use the provider console if SSH remains unavailable. |
| Public `502`, internal health good | Inspect Nginx on the host: `ssh KAG 'systemctl status nginx --no-pager'`. |
| Key file missing | Find the offline backup. Do not create a new key; it cannot unlock the existing database. |
| Key file permissions wrong | Original file should be owned by you with mode 600: `chmod 600 ~/.config/gnotes/recovery/database-v1.key`. |
| Unlock failed with key present or other service error | Read the service logs. Do not delete the database/key or restore an old database as a first step. |

To use an offline backup after losing the original workstation key:

```sh
gnotes-recover --key-file "/path/to/drive/gnotes-recovery/database-v1.key"
```

If the workstation itself is lost, first set up your SSH access and `KAG` alias
on the replacement machine. The database key does not grant SSH access. Keep
your normal SSH recovery arrangements available separately.

## Installation and fallback

The source helper is `deploy/scripts/gnotes-control`. Installed commands in
`~/.local/bin` are `gnotes-check`, `gnotes-recover` and `gnotes-backup-key`.
If the shell cannot find them, use `~/.local/bin/gnotes-check`, etc.

Without installation, from this checkout:

```sh
python3 deploy/scripts/gnotes-control status
python3 deploy/scripts/gnotes-control recover
python3 deploy/scripts/gnotes-control backup-key "/path/to/drive"
```

The original manual unlock command still works from this workstation:

```sh
ssh -T KAG '/home/rick/apps/gnotes/bin/unlock-gnotes-podman' < /home/rick/.config/gnotes/recovery/database-v1.key
```

Keep key contents out of screenshots, chat messages, email, Git and shared logs.
