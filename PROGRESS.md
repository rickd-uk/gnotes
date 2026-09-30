# gnotes progress

Last updated: 2026-09-30

## Current production release

`v0.7.4` is deployed on Kagoya at `https://gnotes.rickd.dev`.

- The release workflow passed Go tests, the race test, `go vet`, ShellCheck, backup and restore tests, and the application rehearsal.
- The Kagoya updater verified the release checksum, created a pre-release SQLite backup, restarted the rootless Podman service, and verified application health.
- The live container is healthy and SQLite is connected.

## Completed in this release

- Full, Fold, and Notes share a row in the wide-screen Tools menu.
- Note titles share their row with the note time and action menu.
- Title and text controls now range from 75% to 150%.
- Note font choices include Writer, Balanced, Clean, Cursive, Technical, and Playful.
- Menu opacity can be lowered to 15%.
- Notes support persistent subtle background colors with readable contrasting text.
- Notes can be archived for later reading and restored from Archived notes without deletion.
- Link underlines can be turned on or off; color-only links underline on hover and focus.
- The administration menu label is shorter: Users & signups.

## Future plans

Priority order remains:

1. Replicate validated backups to encrypted storage outside Kagoya and alert on stale or failed backups, low disk space, service restarts, and TLS expiry.
2. Add note export and import with Markdown, plain text, and a versioned lossless gnotes format.
3. Move inline JavaScript and CSS into versioned assets and remove `unsafe-inline` from the Content Security Policy.
4. Add privacy-conscious audit records for administration and authentication security events.
5. Design password recovery and administrator MFA with recovery codes before implementation.
6. Add bulk archive and restore controls, then consider encrypted offline access and task-checkbox interaction.

End-to-end encryption, horizontal scaling, and a PostgreSQL migration remain later design projects. SQLite remains appropriate for the current single-instance deployment.
