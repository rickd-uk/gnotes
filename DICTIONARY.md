# Personal dictionary

Right-click text in a displayed note or the rich editor for **Copy**, **Dictionary**, and **Always ignore spelling**. Copy uses the full selection, or the clicked word when nothing is selected. Dictionary opens only when chosen. Shift + right-click keeps the browser's native menu; links, code, and source text fields keep their native menus. Selecting text in a displayed note exposes a Dictionary button; the editor's selection toolbar has the same action. Account → Dictionary also accepts typed words and short phrases.

Definitions are grouped by part of speech, with WordNet examples and related words. **Save word** adds the word to your account's list; **Remove saved word** removes it. Saved words can be searched, opened, removed, and exported as JSON. A word without a definition can still be saved.

WordNet provides English nouns, verbs, adjectives, and adverbs. It is a lexical database rather than a complete dictionary of function words, new slang, or specialist terminology. Exception lists and standard suffix rules resolve common inflections. Lookup preserves the selected spelling in the saved list while showing which base form supplied the definition.

## Spellcheck, Names, and ignored words

**Controls → Spellcheck** turns native spelling underlines on or off for note text and titles. **Always ignore** maintains a separate list of individual words; this action is also available from the right-click menu and Dictionary lookup, without saving a dictionary word.

The separate **Names** list accepts people, companies, places, and other names, including full names such as “New York”. Matching names are automatically excluded from native checking in rich note text, ignoring case. Whole words and phrases match; a name does not suppress unrelated words that merely contain it. Inline formatting does not prevent matching. Remove an entry to allow checking again.

Names and ignored words are private account data in SQLite's `spelling_entries` table. They load at sign-in, when opening Spellcheck, and when returning to the browser window, making them available across devices. Each list holds up to 2,000 entries. Additions and removals target individual entries, so one device does not replace another device's list. All endpoints require authentication, writes require CSRF protection, and account deletion cascades to its entries. Consistent SQLite backups and restores include both lists automatically; note exports do not include spelling lists. Clearing browser storage no longer removes lists saved to the account. Spellcheck on/off remains a per-browser preference.

For lists created in v0.7.28–v0.7.29, open the original browser after updating. Sign-in or reload automatically merges those local lists into the account without overwriting existing entries or their categories. Browser copies are retained until the server confirms migration. A retry handles interrupted migration; successful migration removes the old lists from browser UI preferences and records completion so later reloads do not resurrect removed entries. Each existing browser migrates its own lists.

The browser's own dictionary remains responsible for source text fields and titles when spellcheck is on; HTML text fields cannot selectively disable checking for individual words. App ignore lists use editor view decorations, preserving note content and exported Markdown.

## Storage and privacy

The lexicon is embedded in the server binary. There are no runtime dictionary downloads or external lookup requests. Definitions are shared public reference data; saved words are private account data in SQLite's `saved_words` table, keyed by `(user_id, word)`. All endpoints require authentication; save and removal require CSRF protection. Removing an account cascades to its saved words.

Each account can save 10,000 words, with a maximum of 96 characters and six words per entry. List results are paginated at 50 entries. The existing consistent SQLite backups and restores include saved words automatically. Note exports contain notes; **Export words** downloads the personal word list separately. Word-list import is not implemented.

## Data source, license, and updates

Source: [Princeton WordNet 3.1](https://wordnetcode.princeton.edu/wn3.1.dict.tar.gz). Copyright 2011 Princeton University. Its license permits copying, modification, and redistribution with the copyright notice and disclaimer retained. The complete notice is in [public/WORDNET-LICENSE.txt](public/WORDNET-LICENSE.txt), accessible from the dictionary footer and included inside the embedded archive.

The upstream archive SHA-256 is `3f7d8be8ef6ecc7167d39b10d66954ec734280b5bdcd57f7d9eafe429d11c22a`. The bundled archive contains the four data files, four ordered indices, four exception lists, and the license. It is 8,511,106 bytes and provides 147,478 distinct indexed entries after case normalization. Local heap profiling measured approximately 53 MB of retained dictionary allocations. It increases the application binary, release size, and server memory use; the reference dataset is not copied into each SQLite backup. Only saved words add database backup space.

Rebuild from the pinned upstream download:

```sh
curl -fLsS https://wordnetcode.princeton.edu/wn3.1.dict.tar.gz -o /tmp/wordnet-3.1.tar.gz
python3 tools/build-dictionary.py /tmp/wordnet-3.1.tar.gz
go test ./internal/dictionary ./cmd/server
```

The builder verifies the upstream checksum and writes a deterministic archive. Updates are deliberate releases: review the replacement source/license, update the pinned checksum and importer as needed, regenerate the dataset, and rerun lexical, ownership, pagination, and browser tests. The server validates the embedded files before serving requests.
