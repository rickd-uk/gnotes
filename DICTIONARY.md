# Personal dictionary

Right-click a word in a displayed note or the rich editor to look it up. Selecting text in a displayed note exposes a Dictionary button; the editor's selection toolbar has the same action. Account → Dictionary also accepts typed words and short phrases.

Definitions are grouped by part of speech, with WordNet examples and related words. **Save word** adds the word to your account's list; **Remove saved word** removes it. Saved words can be searched, opened, removed, and exported as JSON. A word without a definition can still be saved.

WordNet provides English nouns, verbs, adjectives, and adverbs. It is a lexical database rather than a complete dictionary of function words, new slang, or specialist terminology. Exception lists and standard suffix rules resolve common inflections. Lookup preserves the selected spelling in the saved list while showing which base form supplied the definition.

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
