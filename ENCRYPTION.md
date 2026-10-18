# Database encryption and recovery

Stage 1 adds optional deployment-wide SQLCipher encryption for every account.
It does not add a user setting or end-to-end encryption. Production activation
is a separate migration; installing this code does not silently convert data.

## Protection and key ownership

SQLCipher 4 encrypts database pages with AES-256-CBC and authenticates them with
HMAC-SHA-512. Database contents, schema, search indexes, cached rendered notes,
drafts, sessions and personal-backup keys are inside that encrypted database.
Committed WAL/journal pages are encrypted too. Temporary database work stays in
memory and mmap is disabled on every encrypted connection, including replacement
connections after cancellation. The WAL shared-memory index contains operational
metadata; file sizes, paths and access patterns are not concealed.

The owner generates a random 256-bit database key. The key is supplied in a
separate private file, never a URL, process argument containing the key, log,
browser preference or database table. Keep the recovery copy offline, outside
the server, database directory, backup directories and Git. A key file must be a
regular file with mode 0600 or 0400; symlinks and broadly readable files are
rejected. There is no fallback to plaintext when a supplied key fails. Set
`GNOTES_REQUIRE_ENCRYPTION=1` on encrypted deployments so a missing key fails
startup even if the database file is missing too.

Automatic unlocking needs a server-accessible key file. If that usable key is on
the same stolen drive, encryption does not protect against theft of the whole
drive. Alternatively provision the key into private volatile storage such as
`/run/user/UID/gnotes/database.key` after each host reboot. That allows app
restarts while the volatile key exists, but needs the owner's intervention after
the host reboots. tmpfs can be swapped: use encrypted swap or disable swap and
disable process/container core dumps. This does not implement a web unlock form
or claim that the key exists only in application memory.

A compromised unlocked server can read all retained notes and keys. This is
unchanged by key rotation. Optional per-account end-to-end encryption remains
stage 2. A trustworthy client is needed against an actively malicious web server.

## Selected deployment: key kept off Kagoya's drive

The owner selected volatile key storage on 2026-10-18. Kagoya currently has an
unencrypted swap file, so ordinary tmpfs alone is insufficient. The deployment
profile sets `GNOTES_VOLATILE_KEY=1`: app and database commands disable their core
dumps before reading a key, require a tmpfs/ramfs key file, and refuse active
swap. This protects both the key file and copies of the key/plaintext in process
memory. Do not weaken this guard merely to make startup succeed.

One-time root preparation (script handles no key or note data):

```sh
sudo python3 deploy/scripts/prepare-gnotes-volatile-host --user rick --disable-swap
```

This disables swap for the entire host after checking memory headroom, masks the
discovered swap units against boot-time reactivation, and installs a boot-enabled
64 KiB `noswap` tmpfs mount at `/run/gnotes/keys`, owned by the app account with
mode 0700. The existing swap file is preserved, not wiped; old plaintext remnants
must be considered historical exposure. Review RAM capacity for all host apps.
The script refuses unsupported swap configurations or conflicting mount units.
No key is generated or read by this root step. Re-enabling swap is incompatible
with this profile; startup and operational checks will refuse it.

After activation, the container mounts `/run/gnotes/keys` read only at
`/run/gnotes` and receives:

```sh
GNOTES_DATABASE_KEY_FILE=/run/gnotes/database.key
GNOTES_REQUIRE_ENCRYPTION=1
GNOTES_VOLATILE_KEY=1
```

Host-side backup/control jobs receive the host path
`GNOTES_DATABASE_KEY_FILE=/run/gnotes/keys/database.key` in their private storage
configuration, along with both policy flags. Configure the app Quadlet with
`ConditionPathExists=/run/gnotes/keys/database.key` in `[Unit]`,
`Ulimit=core=0:0` in `[Container]`, and `LimitCORE=0` plus `MemorySwapMax=0` in
`[Service]`. Apply those service memory protections to backup/control jobs too.
The host mount is recreated empty at boot. No startup job retrieves the key
automatically from another service or a file on the server.

To unlock after a Kagoya reboot, run this on the owner's trusted machine:

```sh
ssh -T KAG '/home/rick/apps/gnotes/bin/unlock-gnotes-podman' < /PRIVATE/gnotes-database.key
```

The helper checks non-swappable storage before reading stdin, writes a private
candidate only in RAM, verifies it against the encrypted database, atomically
publishes the correct key and starts gnotes. A wrong key cannot replace an
existing correct key. App restarts and updates can reuse the key while the host
remains up. All accounts wait after a host reboot until the owner unlocks.
`--stage-only` is restricted to the initial plaintext migration, refuses an
existing runtime key, and neither starts nor changes the application.

## Build and validation

Native encryption builds require Go 1.25+, a C compiler, and OpenSSL development
headers/libraries. Use `make build`, `make check`, `make test-encryption`,
`make test-operations`, and `make test-browser` (the last requires ChromeDriver on port 19515). The Makefile
supplies `GOFLAGS=-tags=sqlite_fts5` and
`CGO_CFLAGS='-O2 -g -ftls-model=initial-exec'`. FTS5 is required for note search;
the TLS flag supports SQLCipher's native memory protection with Go's linker.
Do not set `CGO_ENABLED=0` for an encrypted deployment: that compatibility build
can still use plaintext SQLite but explicitly rejects encrypted database opens.

The pinned native driver is `github.com/0xCarbon/go-sqlite3 v1.15.1`, a SQLCipher
fork of mattn's driver, bundling SQLCipher 4.17.0 / SQLite 3.53.3 and using OpenSSL.
Dependencies are checksum-pinned in go.sum. This project has tested its storage,
migration and app integration; that is not an independent cryptographic audit.
Maintain this dependency alongside its upstream engine and OpenSSL updates.
The legacy pure-Go SQLite driver remains for plaintext compatibility and tests.

Releases are built on Ubuntu 24.04 with OpenSSL 3 and run in a matching Ubuntu
24.04 container. Host operational tools need compatible glibc/OpenSSL (Kagoya's
Ubuntu 26.04 host was checked). An arbitrary locally built binary must not be
copied to a host with an older incompatible runtime.

## Generate and test a recovery key

On a trusted machine, with a private existing directory outside the checkout:

```sh
./gnotes db keygen --output /PRIVATE/gnotes-database.key
```

The command creates a mode 0600 file containing 64 hexadecimal characters and
never prints the key. It refuses to overwrite an existing file. Copy the file to
your offline recovery storage and test that copy against a candidate encrypted
database before retiring any plaintext recovery copy. Do not reuse the personal
backup key or the Wasabi Restic password as the database key.

## Migrate existing data

First stage a compatible binary and all updated backup/restore/update tools.
Choose key provisioning and confirm the owner has a usable offline recovery copy.
Stop writes for the final migration: a copy made while users continue writing is
only a point-in-time backup and must not replace newer production data.

With gnotes stopped, create and verify a new encrypted candidate in its private
data directory. The source is read only and is not replaced by these commands:

```sh
./gnotes db encrypt --source /DATA/gnotes.db --output /DATA/gnotes-encrypted.db --key-file /PRIVATE/gnotes-database.key
./gnotes db verify --source /DATA/gnotes-encrypted.db --key-file /OFFLINE-COPY/gnotes-database.key
```

Only after verification, install the candidate as the application database with
mode 0600 and the service's ownership, and configure the service with:

```sh
GNOTES_DATABASE_KEY_FILE=/run/gnotes/database.key
GNOTES_REQUIRE_ENCRYPTION=1
```

For Podman, mount the private key directory read only at `/run/gnotes`; use the
container path in the app configuration. Use the host path in operational jobs.
Supply the same settings to daily-backup and Wasabi/backup-control services with
an appropriate private EnvironmentFile. These jobs do not inherit the app's
container environment. Install the release's `gnotes` executable on the host at
`~/apps/gnotes/bin/gnotes-db`; this executable's `db` subcommands do not start HTTP.
The Podman updater refreshes it after successful releases. Set
`GNOTES_STORAGE_TOOL` if using a different host path.

Do not restore an older application image that cannot read SQLCipher files.
Keep an encryption-capable rollback binary/image together with the key. Schema
migrations and encryption activation must not be conflated with ordinary updates.

Check login, note counts/content, drafts, search, archived/recycled notes,
personal exports and a fresh daily backup/restore before declaring activation
complete. Existing files are not retroactively protected: inventory plaintext
databases, WALs/journals, pre-update/pre-restore backups, old daily archives,
staging files and host/provider snapshots. Convert retained SQLite backups with
`db encrypt`, verify them, and keep encrypted copies in place of plaintext ones.
Decompress old gzip backups only in private volatile/encrypted staging storage.
Retirement of the old plaintext originals is a separate deliberate step after
recovery verification; unlinking a file is not guaranteed secure erasure on
SSDs, snapshots or provider storage. Never claim that activation removes all
historical exposure.

## Back up and recover

Once the key path is supplied to the updated scripts, daily, pre-update and
pre-restore snapshots are SQLCipher databases from their first write. Each copy
uses a fresh random database salt and is checked for authenticated-page integrity
and application schema before publication. Compressed filenames remain `.db.gz`;
gzip is compression, not the source of encryption. Partial snapshots are private
encrypted files and are removed on handled failures. Existing destinations are
never overwritten. New snapshots include committed WAL transactions.

Encrypted database pages compress poorly. Budget local retention space and
Wasabi capacity/traffic for snapshots close to the full database size rather
than the smaller historical plaintext gzip size before activation.

The normal backup/restore scripts keep their existing interfaces. For a manual
encrypted snapshot or verification:

```sh
./gnotes db snapshot --source /DATA/gnotes.db --output /BACKUPS/new-backup.db --key-file /PRIVATE/gnotes-database.key
./gnotes db verify --source /BACKUPS/new-backup.db --key-file /OFFLINE-COPY/gnotes-database.key
```

To recover all accounts on a replacement machine, you need:

1. An encrypted database backup and this database's matching key file.
2. A compatible encryption-capable gnotes binary, public assets and restore tools.
3. Wasabi access credentials and the separate Restic recovery file if retrieving
   the database backup from Wasabi. Restic decryption leaves a database that still
   needs the database key; the existing Profile site-key download is not that key.

Decompress a `.db.gz` into private staging (the decompressed database is still
encrypted), verify it with the owner's key, install it with the correct owner and
permissions, configure the key file and start gnotes. On an existing installation
use the updated restore command, which verifies before stopping the service and
retains encrypted rollback copies. A wrong key cannot replace the live database.
For replacement-host recovery no old database is required: verify and install
the backup directly instead of using the in-place restore script's current-file
guard. Test recovery using the actual offline copy of the key.

Losing the database and all backups cannot be repaired with a key alone. Losing
every copy of the database key makes encrypted databases unrecoverable. A login
password or reset email does not replace this key. Personal `.gnotes-backup`
downloads retain their existing per-account recovery keys and import procedure.

References: [SQLCipher design](https://www.zetetic.net/sqlcipher/design/),
[SQLCipher API/export](https://www.zetetic.net/sqlcipher/sqlcipher-api/), and
[OWASP cryptographic storage](https://cheatsheetseries.owasp.org/cheatsheets/Cryptographic_Storage_Cheat_Sheet.html).
