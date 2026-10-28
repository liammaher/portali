#!/usr/bin/env python3
"""Load the made-up Pizzeria Fittizia into a real business vault, to try its pratiche from a
fresh Claude session as with a real business.

The user, in a terminal (each command asks for the vault's password):

  pvault --vault fittizia init --impresa
  pvault --vault fittizia unlock
  pvault --vault fittizia person add gennaro --ruolo titolare
  pvault --vault fittizia create-service-token fittizia-load --scope '*' --access write \\
    --ttl 1h --keychain pvault-fittizia-load

then:  load.py [--vault-dir ~/.pvault-fittizia] [--keychain pvault-fittizia-load]

It writes fixture.json's fields over the vault's socket with that token, read from the
Keychain as fill.py reads its own, and stops at the first refused write (a personal vault
refuses the first one, the titolare's ruolo). Revoke the token afterwards:
pvault --vault fittizia revoke-service-token <prefix>.
"""

import argparse
import json
import sys
import urllib.parse
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent.parent / "claude" / "skills" / "pratica" / "fill"))
sys.dont_write_bytecode = True  # no __pycache__ in the skill's folder
import fill  # noqa: E402


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--vault-dir", default=str(Path.home() / ".pvault-fittizia"))
    ap.add_argument("--keychain", default="pvault-fittizia-load")
    a = ap.parse_args()
    vault = fill.connect(argparse.Namespace(server=None, vault_dir=a.vault_dir, keychain=a.keychain))["prepare"]
    fields = json.loads((HERE / "fixture.json").read_text())["fields"]
    for f in fields:
        body = {"value": f[1]} | ({"sensitivity": f[2]} if len(f) > 2 else {})
        status, got = vault.call("PUT", "/vault/fields/" + urllib.parse.quote(f[0], safe=":"), body)
        if status != 200:
            sys.exit(f"{f[0]}: {status} {got}")
    print(f"loaded {len(fields)} fields into {a.vault_dir}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
