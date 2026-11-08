# GNotes backup and recovery — simple guide

Keep a copy of this guide outside GNotes, such as an attachment in KeePassXC.

## What you need to keep

| Item | What it does |
|---|---|
| Account password | Signs you into your account. |
| Personal recovery key (`.json`) | Unlocks your downloaded personal note backups. Download it inside GNotes. |
| Personal note backup (`.gnotes-backup`) | Contains a dated copy of your notes. Download a fresh one regularly. |
| Database key (`database-v1.key`) | Unlocks the whole server database after a reboot or during server recovery. This is in your recovery ZIP stored in KeePassXC. |

The recovery ZIP contains the **database key and instructions, not your notes**.
The database key and personal recovery key are different. Changing your account
password does not change either key.

## 1. Back up your notes while the site works

1. Save/finish any unfinished draft you want included.
2. Open the **Profile menu → Backup & recovery → My notes**.
3. Enter your current account password and choose **Download my recovery key**.
   Keep this `.json` file in KeePassXC, labelled **GNotes personal backup key**.
4. Enter your password again and choose **Download my encrypted backup**.
5. Save the `.gnotes-backup` file on secure storage outside the GNotes server.
   Keep the key separately from the note backups.
6. In **My notes → Test a personal backup**, select the backup and the personal
   `.json` key, then choose **Test personal recovery**. Check that it succeeds.
   This test runs in your browser and imports no notes.

The note backup includes active, archived and recycled notes for your account;
it does not include an unfinished draft, other accounts or the server setup.
Download a fresh backup after important writing and keep several dated copies.

Also back up your KeePassXC database to another device or secure external
storage. You must be able to unlock it without using GNotes.

## 2. Restore notes from a downloaded personal backup

Use this when you need notes from an older backup and can sign into GNotes.

1. Sign into the account that should receive the recovered notes.
2. Open **Profile menu → Backup & recovery → My notes → Export & import**.
3. Select your `.gnotes-backup` file in the import file control.
4. Select its personal `.json` key under **Recovery key for an encrypted backup**.
5. Leave **Skip exact matches** selected and choose **Import file**.
6. Check the success message and recovered notes.

Import adds notes; it does not replace the live database or overwrite current
notes. Identical notes are skipped; older versions that differ may be added as
separate copies. This restores note data, not account credentials or server
configuration. Do not select the server's `database-v1.key` in this form.

## 3. Site shows Offline reading after a host reboot

Run these in a terminal **on your workstation**, outside any SSH session:

```sh
gnotes-check
```

If it says the server key is missing:

```sh
gnotes-recover
```

Wait for the success message, then open <https://gnotes.rickd.dev/> again.
Preserve unsaved editor text before reloading an existing tab.

A reboot clears the server's in-memory key; your encrypted notes remain on
disk. Recovery supplies your existing key. This is an **unlock**, not restoration
of an older backup. No old notes are imported or substituted.

If you lost the original workstation key, extract the recovery ZIP from
KeePassXC into a private folder and use its key:

```sh
gnotes-recover --key-file "/path/to/gnotes-recovery/database-v1.key"
```

Replace the example path with the real extracted path. Remove exposed temporary
key files when you have finished; retain the secure KeePassXC attachment.

If the commands are unavailable, the ZIP includes the standalone helper:

```sh
python3 "/path/to/gnotes-recovery/gnotes-control" status
python3 "/path/to/gnotes-recovery/gnotes-control" recover --key-file "/path/to/gnotes-recovery/database-v1.key"
```

On a replacement workstation, install Python 3 and SSH, configure your `KAG`
connection and SSH key first. The database key alone does not grant SSH access.

## 4. If recovery fails

| What you see | What to do |
|---|---|
| SSH access failed / `Permission denied (publickey)` | Run `ssh-add`, then `ssh KAG`. Once connected, type `exit` and retry `gnotes-check`. |
| SSH timed out / connection refused | Check Internet access, then the host provider's console/status. |
| Key missing | Run `gnotes-recover`. Do not generate a new key. |
| Key present, app unavailable | Inspect the app logs with the commands below. |
| App healthy internally, public site unavailable | Inspect Nginx/network/DNS. Repeated unlocking will not fix this. |
| Website healthy, browser still shows Offline reading | Open the main site URL again. |

Read-only service checks:

```sh
ssh KAG 'systemctl --user status gnotes.service --no-pager'
ssh KAG 'journalctl --user -u gnotes.service -n 40 --no-pager'
ssh KAG 'systemctl status nginx --no-pager'
```

Do not delete the database, replace its key, or restore an older database as a
first response to a startup problem.

## 5. If the whole server is lost

The key ZIP alone cannot recreate the server or its notes.

- To recover **your notes**, set up a working GNotes instance/account and import
  your personal backup using section 2. You need the `.gnotes-backup` and its
  personal `.json` key.
- To recover **every account and its server-stored data**, you need a completed
  whole-database backup and the matching `database-v1.key`, plus a replacement
  server/application setup. Wasabi backups also need storage credentials and
  the off-server storage recovery file. That file does not replace the database
  key. Follow the full procedures in OPERATIONS.md and ENCRYPTION.md.

Check **Backup & recovery → Overview** for the most recent successful local and
off-server backups and recovery tests. Local backups on the same server do not
protect against losing that server. Do not assume off-server scheduling is on;
confirm it in Overview/Off-server before relying on it.
