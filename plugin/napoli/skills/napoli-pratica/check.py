#!/usr/bin/env python3
"""Check a job's values the way the vault would, then merge them for run.py.

  check.py STUDIO.json JOB.json --out VALUES.json

STUDIO.json is the studio and the architect (written once, kept at the folder's root);
JOB.json is this job's people (the client, the owner, the firms). Both are flat JSON:
field id -> value, ids as in reference/schema.json (`slug:category.field`, no slug for the
studio itself). Prints each problem and exits 1, or writes VALUES.json (the two merged,
values normalized: codes upper-case, dates YYYY-MM-DD) for `run.py --values`.

The plugin has no vault, so this holds the values to the same checks the vault's
NormalizeAndValidate makes for what the portal reads (internal/vault/italy.go): the
portal refuses a wrong codice fiscale only once the run is well under way.
"""

import argparse
import datetime as dt
import json
import re
import sys
from pathlib import Path

SCHEMA = Path(__file__).resolve().parent / "reference" / "schema.json"
ID = re.compile(r"^(?:([a-z][a-z0-9-]{0,31}):)?([a-z][a-z0-9_]*?)(?:-?\d+)?\.([a-z0-9_]+)$")
CF_ODD = dict(zip("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ",
                  [1, 0, 5, 7, 9, 13, 15, 17, 19, 21, 1, 0, 5, 7, 9, 13, 15, 17, 19, 21,
                   2, 4, 18, 20, 11, 3, 6, 8, 12, 14, 16, 10, 22, 25, 24, 23]))
CF_MONTHS = "ABCDEHLMPRST"
CF_OMOCODIA = "LMNPQRSTUV"
ADDRESSES = ("residenza", "domicilio", "locale", "immobile")


def code(v):
    return re.sub(r"[\s.\-]", "", v.upper())


def partita_iva(p):
    if not re.fullmatch(r"\d{11}", p):
        return "must be 11 digits"
    total = 0
    for i, c in enumerate(p[:10]):
        d = int(c) * (2 if i % 2 else 1)
        total += d - 9 if d > 9 else d
    if (10 - total % 10) % 10 != int(p[10]):
        return "check digit does not match"
    if p[7:10] == "000":
        return "office code (digits 8-10) can't be 000"
    return None


def codice_fiscale(cf):
    if len(cf) == 11:
        return partita_iva(cf) and "11-digit codice fiscale has an invalid check digit"
    if not re.fullmatch(r"[A-Z]{6}[0-9LMNPQRSTUV]{2}[A-Z][0-9LMNPQRSTUV]{2}[A-Z][0-9LMNPQRSTUV]{3}[A-Z]", cf):
        return "must be 16 characters (or 11 digits) in the codice fiscale's pattern"
    digits = "".join(c if c.isdigit() else str(CF_OMOCODIA.index(c)) for c in cf[6:8] + cf[9:11])
    month = CF_MONTHS.find(cf[8])
    if month < 0:
        return f"invalid month letter {cf[8]!r}"
    year, day = int(digits[:2]), int(digits[2:])
    day -= 40 if day > 40 else 0  # women have 40 added to the day of birth
    if not 1 <= day <= [31, 29 if year % 4 == 0 else 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month]:
        return "invalid day of birth"
    total = sum(CF_ODD[c] if i % 2 == 0 else (int(c) if c.isdigit() else ord(c) - 65) for i, c in enumerate(cf[:15]))
    return None if cf[15] == chr(65 + total % 26) else "check character does not match"


def is_date_field(name):
    words = name.split("_")
    return any((w == "data" and i < len(words) - 1) or (w == "date" and i == len(words) - 1 and i > 0)
               or (w == "date" and i < len(words) - 1 and words[i + 1] == "of") for i, w in enumerate(words))


def address_part(category, name, part):
    return name == part if category in ADDRESSES else category == "business" and name == "sede_legale_" + part


def normalize(category, name, v):
    """(value, problem) for one field, as the vault's NormalizeAndValidate would."""
    if name == "codice_fiscale" or name.endswith("_codice_fiscale"):
        v = code(v)
        return v, codice_fiscale(v)
    if name == "partita_iva" or name.endswith("_partita_iva"):
        v = code(v).removeprefix("IT")
        return v, partita_iva(v)
    if address_part(category, name, "cap"):
        v = v.strip()
        return v, None if re.fullmatch(r"\d{5}", v) else "CAP must be 5 digits"
    if address_part(category, name, "provincia") or (category == "identity" and name == "birth_province"):
        v = code(v)
        return v, None if re.fullmatch(r"[A-Z]{2}", v) else "provincia must be a 2-letter sigla (e.g. NA)"
    if category == "identity" and name == "sex":
        v = v.strip().upper()
        return v, None if v in ("M", "F") else "sex is M or F, as on Italian documents"
    if is_date_field(name):
        v = v.strip()
        m = re.fullmatch(r"(\d{1,2})[/.-](\d{1,2})[/.-](\d{4})", v)
        if m:  # 05/03/1980, as Italian documents write it
            v = f"{m[3]}-{int(m[2]):02d}-{int(m[1]):02d}"
        try:
            dt.date.fromisoformat(v)
            return v, None
        except ValueError:
            return v, "a date, YYYY-MM-DD"
    return v.strip(), None


def check(fields, schema):
    known = {f["id"]: f for c in schema["categories"] for f in c["fields"]}
    secret = re.compile(schema["credentials"]["secret"])
    out, problems = {}, []
    for fid, v in fields.items():
        m = ID.match(fid)
        if not m:
            problems.append(f"{fid}: not a field id (slug:category.field, lower case)")
            continue
        _, category, name = m.groups()
        if any(secret.match(w) for w in category.split("_") + name.split("_")):
            problems.append(f"{fid}: a password or PIN never goes in these files")
            continue
        if f"{category}.{name}" not in known:
            problems.append(f"{fid}: no field {category}.{name} in reference/schema.json")
            continue
        if not isinstance(v, str):
            v = "true" if v is True else "false" if v is False else str(v)
        v, problem = normalize(category, name, v)
        allowed = known[f"{category}.{name}"].get("values")
        if not problem and allowed:
            bad = [p.strip() for p in v.split(",") if p.strip() not in allowed]
            problem = bad and f"{', '.join(bad)} not one of {', '.join(allowed)}"
        if problem:
            problems.append(f"{fid}: {problem} (got {fields[fid]!r})")
        out[fid] = v
    return out, problems


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("studio", type=Path)
    ap.add_argument("job", type=Path)
    ap.add_argument("--out", type=Path, required=True)
    a = ap.parse_args()
    studio, job = json.loads(a.studio.read_text()), json.loads(a.job.read_text())
    both = [k for k in job if k in studio]
    if both:
        print("in both files (the studio's are kept in studio.json only): " + ", ".join(both))
        return 1
    values, problems = check(studio | job, json.loads(SCHEMA.read_text()))
    for p in problems:
        print(p)
    if problems:
        return 1
    a.out.parent.mkdir(parents=True, exist_ok=True)
    a.out.write_text(json.dumps(values, indent=1, ensure_ascii=False))
    print(f"{a.out}: {len(values)} values")
    return 0


if __name__ == "__main__":
    sys.exit(main())
