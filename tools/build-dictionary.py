#!/usr/bin/env python3
"""Repack the pinned upstream WordNet archive for the embedded offline lexicon."""
import gzip
import hashlib
import io
from pathlib import Path
import re
import sys
import tarfile

SOURCE_SHA256 = "3f7d8be8ef6ecc7167d39b10d66954ec734280b5bdcd57f7d9eafe429d11c22a"
root = Path(__file__).resolve().parents[1]
source = Path(sys.argv[1]).read_bytes()
if hashlib.sha256(source).hexdigest() != SOURCE_SHA256:
    raise SystemExit("Unexpected upstream archive checksum")
names = [f"{kind}.{pos}" for pos in ("noun", "verb", "adj", "adv") for kind in ("data", "index")]
names += [f"{pos}.exc" for pos in ("noun", "verb", "adj", "adv")]
with tarfile.open(fileobj=io.BytesIO(source), mode="r:gz") as upstream:
    files = {name: upstream.extractfile("dict/" + name).read() for name in names}
license_text = "\n".join(re.sub(r"^\s*\d+ ?", "", line).rstrip()
                         for line in files["data.noun"].decode().splitlines()[:29]) + "\n"
license_text += "\nBundled in gnotes: data, indices, and exception lists from WordNet 3.1.\n"
license_text += "Source: https://wordnetcode.princeton.edu/wn3.1.dict.tar.gz\n"
(root / "public/WORDNET-LICENSE.txt").write_text(license_text)
files["LICENSE"] = license_text.encode()
destination = root / "internal/dictionary/wordnet.tar.gz"
destination.parent.mkdir(parents=True, exist_ok=True)
with destination.open("wb") as output, gzip.GzipFile(filename="", fileobj=output, mode="wb", mtime=0) as compressed:
    with tarfile.open(fileobj=compressed, mode="w") as archive:
        for name, data in sorted(files.items()):
            info = tarfile.TarInfo(name)
            info.size = len(data)
            info.mode = 0o644
            archive.addfile(info, io.BytesIO(data))
print(f"Bundled dictionary: {destination.stat().st_size:,} bytes")
