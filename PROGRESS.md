# gnotes status

Updated: 2026-10-06

`v0.7.36` deployed: https://gnotes.rickd.dev. Spellcheck now includes search for personal Names and ignored words, lists displayed in batches of 50, JSON export, and JSON/text import. Imports merge without replacing existing entries or name types; validation and server failures preserve the chosen file for retry. Exports include the complete account lists regardless of search filters. Browser checks at 320px and 1024px covered search, pagination, imports, failure/retry, actual downloaded exports, and account isolation. All browser workflows, standard tests, release checks, restore rehearsal, validated backup, live page/style checksums, and health checks passed. Real Pixel 6a verification remains open.

Selected words on phones retain the bottom Dictionary and Always ignore bar above editor Copy/Done controls, with native selection and Copy available. The 15,981 shared names and timezone persistence fix remain included. The pre-update backup is `/home/rick/apps/gnotes/backups/gnotes-pre-v0.7.36-20261006T130004Z.db`.

Phone/tablet orientation checks also passed in Chromium touch emulation: 360×800, 800×360, 768×1024, and 1024×768. Read-view and editor actions stay within the visible viewport and clear of Copy/Done; rotating an existing selection preserves it. These checks require no production code change.

Session handoff: the connected Pixel 6a loaded the live site in Chrome, but native selection and bottom word actions were not verified; screen sleep and blank captures prevented a reliable result. Full device checks remain open. PDF/EPUB previews, the ascending/descending button height, and video thumbnail reports were for other apps; no gnotes changes were made for them.

Remaining:

- Verify the live admin recovery email in Profile and test a real reset email.
- Retest on Pixel 6a browsers.
- Configure encrypted off-server backups and alerts; improve CSP, auditing, and admin MFA.

Details: [ROADMAP.md](ROADMAP.md) · [OPERATIONS.md](OPERATIONS.md). Local: http://127.0.0.1:8080.
