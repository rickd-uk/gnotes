# gnotes status

Updated: 2026-10-07

`v0.7.37` deployed: https://gnotes.rickd.dev. PC mouse/trackpad right-click takes priority over touch detection. The three-item Copy/Dictionary/Always ignore menu covers source editors and rich-editor whitespace, with unavailable actions disabled. The editor's bottom-left Copy uses the current highlighted body/source/title text; without a selection it copies the current title and body separated by a blank line. Pointer interactions preserve the selection until Copy reads it.

Done and outside-click close the editor and display edits immediately, with serialized background writes. Pending edits survive reopening and list refreshes; failed saves retain the attempted text, reopen the editor when available, and show a retry message. Single-note pin, color, archive/unarchive, and recycle changes appear immediately and roll back on failure. Bulk moves wait for outstanding edit saves. New delayed-request browser checks cover rapid re-editing, actual persistence, failure/retry, rollback, and blocking bulk archive after a failed edit save. All Go/browser tests, vet, syntax and diff checks passed, including focused checks after the final bulk-save guard. Narrow-screen fixes hide the closed Markdown guide and let the note action row wrap while keeping its menu aligned within the viewport. Release workflow 37592419493 passed all release checks and the restore rehearsal. Release archive, running binary, and public page/script checksums match; service and public health checks passed. The pre-update backup `/home/rick/apps/gnotes/backups/gnotes-pre-v0.7.37-20261007T081839Z.db` passed integrity and schema verification. New tests: `cmd/server/optimistic_browser_test.go`.

The v0.7.36 spellcheck additions remain included: search for personal Names and ignored words, lists displayed in batches of 50, JSON export, and JSON/text import. Imports merge without replacing existing entries or name types; validation and server failures preserve the chosen file for retry. Exports include the complete account lists regardless of search filters. Browser checks at 320px and 1024px covered search, pagination, imports, failure/retry, actual downloaded exports, and account isolation. All browser workflows, standard tests, release checks, restore rehearsal, validated backup, live page/style checksums, and health checks passed. Real Pixel 6a verification remains open.

Selected words on phones retain the bottom Dictionary and Always ignore bar above editor Copy/Done controls, with native selection and Copy available. The 15,981 shared names and timezone persistence fix remain included. The pre-update backup is `/home/rick/apps/gnotes/backups/gnotes-pre-v0.7.36-20261006T130004Z.db`.

Phone/tablet orientation checks also passed in Chromium touch emulation: 360×800, 800×360, 768×1024, and 1024×768. Read-view and editor actions stay within the visible viewport and clear of Copy/Done; rotating an existing selection preserves it. These checks require no production code change.

Session handoff: the connected Pixel 6a loaded the live site in Chrome, but native selection and bottom word actions were not verified; screen sleep and blank captures prevented a reliable result. Full device checks remain open. PDF/EPUB previews, the ascending/descending button height, and video thumbnail reports were for other apps; no gnotes changes were made for them.

Remaining:

- Favorites proposal: a separate important-note collection, category filters using existing tags, and the user-selected diamond icon (outline when off, muted gold when favorited). This is a design discussion; no Favorites feature has been implemented.
- Verify the live admin recovery email in Profile and test a real reset email.
- Retest on Pixel 6a browsers.
- Configure encrypted off-server backups and alerts; improve CSP, auditing, and admin MFA.

Details: [ROADMAP.md](ROADMAP.md) · [OPERATIONS.md](OPERATIONS.md). Local: http://127.0.0.1:8080.
