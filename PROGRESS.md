# gnotes status

Updated: 2026-10-06

`v0.7.30` deployed: https://gnotes.rickd.dev. Names and ignored words now live in private account-owned SQLite lists, available across devices and covered by database backups. Existing browser lists merge automatically on sign-in/reload and remain intact until migration succeeds. Individual writes avoid replacing another device's additions. Spellcheck on/off remains browser-local. Desktop/mobile checks, migration retries and failures, isolation and limits, Go/race/vet, release restore rehearsal, validated backup, live asset checksums, authentication, schema, and health checks passed; 4 accounts and 37 notes preserved.

Remaining:

- Verify the live admin recovery email in Profile and test a real reset email.
- Retest on Pixel 6a browsers.
- Configure encrypted off-server backups and alerts; improve CSP, auditing, and admin MFA.

Details: [ROADMAP.md](ROADMAP.md) · [OPERATIONS.md](OPERATIONS.md). Local: http://127.0.0.1:8080.
