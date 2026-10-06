#!/usr/bin/env python3
"""Build a shared reference list from the already licensed WordNet bundle."""
import json
import re
import tarfile
from pathlib import Path

root = Path(__file__).resolve().parents[1]
names = set()
# Named people, locations, groups/organizations, and natural objects.
categories = {"14", "15", "17", "18"}
with tarfile.open(root / "internal/dictionary/wordnet.tar.gz") as archive:
    for line in archive.extractfile("data.noun").read().decode().splitlines():
        if not line or line.startswith(" "):
            continue
        fields = line.partition("|")[0].split()
        if fields[1] not in categories:
            continue
        for i in range(int(fields[3], 16)):
            name = fields[4 + 2 * i].replace("_", " ")
            if (2 <= len(name) <= 96 and len(name.split()) <= 12
                    and any(c.isupper() for c in name)
                    and re.fullmatch(r"[^\W\d_][\w ’'-]*", name)):
                names.add(name)
# Modern company/organization names supplement the 2011 reference data.
names.update("""OpenAI|Anthropic|Google|Alphabet|Microsoft|Apple|Amazon|Meta|Facebook|Instagram|WhatsApp|YouTube|TikTok|ByteDance|Nvidia|AMD|Intel|Qualcomm|Broadcom|Samsung|Sony|Panasonic|Nintendo|Sega|Toyota|Honda|Nissan|Mazda|Subaru|Suzuki|Mitsubishi|Toshiba|Hitachi|Fujitsu|NEC|Canon|Nikon|Epson|SoftBank|Rakuten|Mercari|Tencent|Alibaba|Baidu|Huawei|Xiaomi|Lenovo|Asus|Acer|Dell|HP|IBM|Oracle|Salesforce|Adobe|Spotify|Netflix|Disney|Pixar|PayPal|Stripe|Shopify|GitHub|GitLab|Dropbox|Slack|Notion|Reddit|Mozilla|Wikimedia|Wikipedia|SpaceX|Tesla|Uber|Airbnb|Booking|Expedia|IKEA|Lego|Nestle|Unilever|PepsiCo|Coca-Cola|McDonald's|Starbucks|Walmart|Costco|Target|Visa|Mastercard|JPMorgan|Goldman Sachs|Morgan Stanley|Bloomberg|Reuters|BBC|CNN|NHK|Siemens|Bosch|SAP|Volkswagen|BMW|Mercedes-Benz|Audi|Porsche|Ferrari|Renault|Hyundai|Kia|BYD|Geely|Volvo|Ericsson|Nokia|Philips|ASML|TSMC""".split("|"))
data = {"version": 1, "source": "WordNet 3.1 and gnotes company additions", "names": sorted(names)}
destination = root / "public/builtin-names.json"
destination.write_text(json.dumps(data, ensure_ascii=False, separators=(",", ":")) + "\n")
print(f"{len(names):,} built-in names; {destination.stat().st_size:,} bytes")
