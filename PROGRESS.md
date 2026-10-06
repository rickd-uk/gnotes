# gnotes status

Updated: 2026-10-06

`v0.7.34` deployed: https://gnotes.rickd.dev. Phone long presses retain native text selection and Copy, and selection drags take priority over note swipes. Spellcheck includes 15,981 shared built-in names stored on the VPS, loaded once per page and matched locally, with an independent switch and private personal additions. The v0.7.33 timezone fix prevents older tabs from overwriting the chosen timezone during unrelated settings saves; existing browser preferences migrate automatically. All browser workflows, standard tests, release checks, restore rehearsal, validated backups, live asset checksums, and health checks passed. Real Pixel 6a verification remains open.

Remaining:

- Verify the live admin recovery email in Profile and test a real reset email.
- Retest on Pixel 6a browsers.
- Configure encrypted off-server backups and alerts; improve CSP, auditing, and admin MFA.

Details: [ROADMAP.md](ROADMAP.md) · [OPERATIONS.md](OPERATIONS.md). Local: http://127.0.0.1:8080.
