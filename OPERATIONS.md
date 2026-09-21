# gnotes operations

This is the short runbook for the production installation.

## Production inventory

| Item | Value |
|---|---|
| SSH host | `hz-sin` |
| Public URL | `https://gnotes.futureisnear.dev` |
| Health URL | `https://gnotes.futureisnear.dev/api/health` |
| systemd service | `gnotes.service` |
| Application | `/opt/gnotes` |
| Private listener | `127.0.0.1:8081` |
| Database | `/var/lib/gnotes/gnotes.db` |
| Environment | `/etc/gnotes/gnotes.env` |
| Backups | `/var/backups/gnotes` |
| Nginx site | `/etc/nginx/sites-available/gnotes` |

Passwords, private keys, environment files, databases, and backups must never be committed or attached to a GitHub release.

## Normal release and update

Create and push an annotated version tag from a clean, synchronized `main` branch:

```bash
git status --short --branch
git tag -a v0.2.0 -m "gnotes v0.2.0"
git push origin v0.2.0
```

The Release workflow tests the application and publishes a static Linux archive plus `SHA256SUMS`. Wait for the GitHub Actions run and release to succeed before updating production.

On the server, install the exact version:

```bash
ssh -t hz-sin 'sudo /opt/gnotes/bin/update-gnotes v0.2.0'
```

The updater verifies the checksum, runs the database backup service, preserves the previous binary and browser assets, installs both together, restarts systemd, checks local health, and automatically restores the previous application files if verification fails.

Run the complete operational check afterward:

```bash
ssh -t hz-sin 'sudo /opt/gnotes/bin/check-gnotes https://gnotes.futureisnear.dev/api/health'
```

Finally, sign in through a browser and inspect several notes.

## Routine commands

```bash
ssh hz-sin 'systemctl status gnotes --no-pager'
ssh hz-sin 'curl -fsS http://127.0.0.1:8081/api/health'
ssh -t hz-sin 'sudo journalctl -u gnotes -n 100 --no-pager'
ssh -t hz-sin 'sudo systemctl restart gnotes'
ssh -t hz-sin 'sudo systemctl start gnotes-backup.service'
ssh -t hz-sin 'sudo ls -lht /var/backups/gnotes | head'
```

## Manual application rollback

The updater retains one previous binary and asset directory. To restore them:

```bash
ssh hz-sin
sudo systemctl stop gnotes
sudo install -o root -g root -m 0755 /opt/gnotes/gnotes.rollback /opt/gnotes/gnotes.new
sudo mv /opt/gnotes/gnotes.new /opt/gnotes/gnotes
sudo rm -rf /opt/gnotes/public
sudo cp -a /opt/gnotes/public.rollback /opt/gnotes/public
sudo systemctl start gnotes
curl -fsS http://127.0.0.1:8081/api/health
```

Application rollback does not reverse database migrations. Releases that change the schema must use backward-compatible migrations. Use a validated database backup and the restore procedure in `SECURITY.md` when a database rollback is required.

## Failed release

If GitHub Actions fails, do not update production and do not move the version tag. Correct the problem, commit it, and publish a new patch version such as `v0.2.1`.

If the updater fails before replacement, production remains unchanged. If post-install verification fails, it attempts application rollback and prints the commands needed for investigation.

## Certificate and backup checks

```bash
ssh -t hz-sin 'sudo certbot renew --dry-run'
ssh hz-sin 'systemctl list-timers --all gnotes-backup.timer'
```

Local backups protect against application mistakes but not total server loss. Replicate `/var/backups/gnotes` to encrypted storage outside this server.
