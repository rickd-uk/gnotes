# gnotes status

Updated: 2026-10-06

`v0.7.35` deployed: https://gnotes.rickd.dev. Selected words on phones expose Dictionary and Always ignore in a bottom bar, above editor Copy/Done controls; Dictionary includes Save word. Native text selection and Copy remain available, and the floating app formatting menu is hidden during phone selections; Aa still provides formatting. The 15,981 shared names and timezone persistence fix remain included. All browser workflows, touch-tap checks, standard tests, release checks, restore rehearsal, validated backup, live page/script/style checksums, and health checks passed. Real Pixel 6a verification remains open.

Phone/tablet orientation checks also passed in Chromium touch emulation: 360×800, 800×360, 768×1024, and 1024×768. Read-view and editor actions stay within the visible viewport and clear of Copy/Done; rotating an existing selection preserves it. These checks require no production code change.

Remaining:

- Verify the live admin recovery email in Profile and test a real reset email.
- Retest on Pixel 6a browsers.
- Configure encrypted off-server backups and alerts; improve CSP, auditing, and admin MFA.

Details: [ROADMAP.md](ROADMAP.md) · [OPERATIONS.md](OPERATIONS.md). Local: http://127.0.0.1:8080.
