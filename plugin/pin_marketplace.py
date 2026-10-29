#!/usr/bin/env python3
"""Writes .claude-plugin/marketplace.json for one release: the marketplace people add
(`claude plugin marketplace add liammaher/personal-vault-italia`) and the one entry in it, the
napoli plugin, as a zip attached to that release and pinned by its SHA-256, so Claude Code
refuses a download that isn't the file this release published.

  pin_marketplace.py --version 0.1.0 --sha256 <64 hex of napoli-0.1.0.zip> [--out FILE]

The release workflow (.github/workflows/release.yml) runs it after attaching the zip and opens a
pull request with the result; until the first release the repository has no marketplace file,
because there is nothing for it to point at.
"""

import argparse
import json
import re
import sys
from pathlib import Path

REPO = "liammaher/personal-vault-italia"
HERE = Path(__file__).resolve().parent


def marketplace(version, sha256, repo=REPO):
    return {
        "name": "pratiche-italia",
        "description": "Italian bureaucracy for Claude: prepare pratiche on government portals up to the steps only "
                       "you can take (SPID login, declarations, signature, payment, sending).",
        "owner": {"name": "Liam Maher"},
        "plugins": [{
            "name": "napoli",
            "description": "Prepares Comune di Napoli pratiche (SUE and SUAP on impresainungiorno) from your "
                           "documents or your private vault, with the pratiche-napoli library of rules.",
            "source": {
                "source": "archive",
                "url": f"https://github.com/{repo}/releases/download/v{version}/napoli-{version}.zip",
                "sha256": sha256,
            },
        }],
    }


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--version", required=True)
    ap.add_argument("--sha256", required=True)
    ap.add_argument("--out", type=Path, default=HERE.parent / ".claude-plugin" / "marketplace.json")
    a = ap.parse_args()
    if not re.fullmatch(r"\d+\.\d+\.\d+([-+][0-9A-Za-z.-]+)?", a.version):
        ap.error(f"--version {a.version!r} isn't a version like 0.1.0")
    if not re.fullmatch(r"[0-9a-fA-F]{64}", a.sha256):
        ap.error("--sha256 must be 64 hex characters")
    a.out.parent.mkdir(parents=True, exist_ok=True)
    a.out.write_text(json.dumps(marketplace(a.version, a.sha256.lower()), indent=2, ensure_ascii=False) + "\n")
    print(a.out)
    return 0


if __name__ == "__main__":
    sys.exit(main())
