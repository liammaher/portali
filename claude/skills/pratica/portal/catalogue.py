#!/usr/bin/env python3
"""The portal catalogue's bookkeeping, around crawl.js's batch (`__crawl.batch`).

    catalogue.py ops sue|suap [--sectors N,M] [--common]   the batch's `ops`, as JSON
    catalogue.py known                                     fingerprints of the stored crawls
    catalogue.py import EXPORT.json                        a batch's results into sections/
    catalogue.py count                                     distinct sections and their uses
    catalogue.py drift EXPORT.json                         a check batch against the stored crawls

A section's fingerprint is what it shows before any choice (ids, kinds, labels, lengths,
short lists), hashed as crawl.js does: the same fingerprint is the same section, whatever
pratica it sits in, and a different one for a stored section means the portal changed it.
"""
import argparse
import collections
import hashlib
import json
import os
import re
import sys
import unicodedata
from pathlib import Path

HERE = Path(__file__).resolve().parent
CAT = HERE / "catalogue" / "F839"
SECTIONS = CAT / "sections"
DECLARATIONS = re.compile(r"consapevolezza|informativa sulla privacy|requisiti di o|altre dichiarazioni|"
                          r"dichiarazioni ai sensi dell'art\. 6|rispetto sulle tasse|art\. ?75", re.I)
# The SUE's pratiche crawled one by one before the batch, by the names recipes use.
SUE_NAMES = {
    "Comunicazione di Inizio Lavori per opere temporanee (C.I.L.)": "cil",
    "Comunicazione Inizio Lavoro Asseverata (C.I.L.A.)": "cila",
    "Segnalazione Certificata di Inizio Attività Edilizia (S.C.I.A.)": "scia",
    "Comunicazione inizio lavori": "inizio-lavori",
    "Comunicazione di fine lavori": "fine-lavori",
    "Segnalazione Certificata per l'Agibilità": "agibilita",
}


def sportello(name):
    """A crawl's sportello, by its name: the batch's start with it, the first ones are SUE's
    but for the hairdresser's."""
    return "suap" if name.startswith("suap") or name == "acconciatore" else "sue"


def fingerprint(fields):
    basis = [[f["id"], f["kind"], f["label"], f.get("context") or "", f.get("max") or 0,
              f["options"] if isinstance(f.get("options"), list) else []]
             for f in fields if f["kind"] != "message"]
    raw = json.dumps(basis, ensure_ascii=False, separators=(",", ":"))
    return hashlib.sha256(raw.encode()).hexdigest()[:16]


def slug(s, n=48):
    s = unicodedata.normalize("NFKD", s).encode("ascii", "ignore").decode().lower()
    s = re.sub(r"\([^)]*\)", "", s)
    return re.sub(r"[^a-z0-9]+", "-", s).strip("-")[:n].strip("-")


def crawls():
    return {p.stem: json.loads(p.read_text()) for p in sorted(SECTIONS.glob("*.json"))}


def resolve(ref, cache=None):
    """`pratica#section` to that crawled section."""
    cache = cache if cache is not None else {}
    name, _, section = ref.partition("#")
    if name not in cache:
        cache[name] = json.loads((SECTIONS / f"{name}.json").read_text())
    return next(s for s in cache[name]["sections"] if s["section"] == section)


def cmd_ops(args):
    ops, seen = ops_of(args), collections.Counter()
    for o in ops:  # two labels can slug alike
        seen[o["name"]] += 1
        if seen[o["name"]] > 1:
            o["name"] += f"-{seen[o['name']]}"
    return ops


def ops_of(args):
    if args.sportello == "sue":
        w = json.loads((CAT / "sue-wizard.json").read_text())
        have = {p.stem for p in SECTIONS.glob("*.json")}
        ops = []
        for o in w["operations"]:
            name = SUE_NAMES.get(o["label"]) or "sue-" + slug(o["label"], 72)
            if name in have and not args.all:
                continue
            ops.append({"name": name, "sector": w["sector"], "path": f"{o['group']} › {o['label']}"})
        return ops
    w = json.loads((CAT / "suap-wizard.json").read_text())
    sectors = w["sectors"]
    nth = collections.Counter()
    for s in sectors:
        s["nth"] = nth[s["sector"]]
        nth[s["sector"]] += 1
    pick = [int(x) for x in args.sectors.split(",")] if args.sectors else range(len(sectors))
    ops = []
    if args.common:
        # The operations most sectors offer, once each, under the first sector given.
        s = sectors[pick[0]]
        for o in w["common"]:
            ops.append({"name": "suap-common-" + slug(o.split(" › ", 1)[-1], 60), "sector": s["sector"],
                        "sector_nth": s["nth"], "path": o})
        return ops
    for i in pick:
        s = sectors[i]
        for o in s["operations"]:
            ops.append({"name": f"suap-{i:03d}-" + slug(o.split(": ", 1)[1].split(" › ")[-1], 50),
                        "sector": s["sector"], "sector_nth": s["nth"], "path": o})
    return ops


def cmd_known(_):
    known = {}
    for name, c in crawls().items():
        for s in c["sections"]:
            if s.get("fields") and not s.get("error") and not s.get("same_as"):
                known.setdefault(fingerprint(s["fields"]), f"{name}#{s['section']}")
    return known


def cmd_import(args):
    """Each pratica of an export (localStorage `crawl:op:*`) to sections/<name>.json."""
    data = json.loads(Path(args.export).read_text())
    out = []
    for key, r in sorted(data.items()):
        if not key.startswith("crawl:op:"):
            continue
        r = r if isinstance(r, dict) else json.loads(r)
        if r.get("error"):
            out.append(f"{r['name']}: not imported ({r['error']})")
            continue
        # The login's name and CF must never be in a crawl (crawl.js stores only that a box was
        # prefilled): CATALOGUE_PRIVATE is a pattern for them, kept out of the repo.
        private = os.environ.get("CATALOGUE_PRIVATE")
        if private and re.search(private, json.dumps(r, ensure_ascii=False), re.I):
            sys.exit(f"{r['name']}: holds the login's name or CF, not imported")
        doc = {"crawled": r["crawled"], "by": "crawl.js batch, depth 2",
               "pratica": f"{sportello(r['name']).upper()}: {r['operation']}"
                          + (f" (sector {r['sector']})" if r.get("sector") else ""),
               "seconds": r.get("seconds")}
        for k in ("refused", "pratica", "screens", "variants"):
            if k in r:
                doc["generated" if k == "pratica" else k] = r[k]
        doc["sections"] = r["sections"]
        (SECTIONS / f"{r['name']}.json").write_text(json.dumps(doc, ensure_ascii=False, indent=1) + "\n")
        drift = CAT / "drift.json"  # a new crawl settles what the last check found
        if drift.exists():
            d = json.loads(drift.read_text())
            if d["pratiche"].pop(r["name"], None) is not None:
                drift.write_text(json.dumps(d, ensure_ascii=False, indent=1) + "\n")
        new = sum(1 for s in r["sections"] if s.get("fields"))
        out.append(f"{r['name']}: {len(r['sections'])} sections, {new} new")
    return out


def covered(section, dictionary):
    """Whether the dictionary fills every box of this crawled section (gen.py's own test),
    for the person or for the business declaring."""
    import gen
    if DECLARATIONS.search(section["section"]):
        return True
    for subject in (None, "impresa"):
        for entry, as_name in gen.dictionary_entries(dictionary["sections"], section["section"]):
            if entry.get("subject", subject) != subject:
                continue
            if entry.get("omit"):
                return True
            _, problems, _ = gen.fill(entry, as_name, section, {"subject": subject} if subject else {}, section["section"])
            if not problems:
                return True
    return False


def cmd_count(args):
    """Each distinct section (by fingerprint), how many pratiche use it, and whether the
    dictionary fills it."""
    import gen
    dictionary = gen.load_yaml(HERE / "dictionary" / "fnsuap.yaml")
    uses, names, where, cache = collections.Counter(), collections.defaultdict(set), {}, {}
    pratiche = collections.defaultdict(set)
    for name, c in crawls().items():
        cache[name] = c
        sp = sportello(name)
        for s in c["sections"]:
            if s.get("skipped") or s.get("error"):
                continue
            if s.get("same_as"):
                fp = s.get("fingerprint") or fingerprint(resolve(s["same_as"], cache)["fields"])
            else:
                fp = s.get("fingerprint") or fingerprint(s["fields"])
                where.setdefault(fp, f"{name}#{s['section']}")
            uses[fp] += 1
            names[fp].add(s["section"])
            pratiche[fp].add(sp)
    rows = []
    for fp, n in uses.most_common():
        sec = resolve(where[fp], cache) if fp in where else None
        rows.append({"fingerprint": fp, "uses": n, "names": sorted(names[fp]), "crawl": where.get(fp),
                     "sportelli": sorted(pratiche[fp]), "covered": bool(sec) and covered(sec, dictionary)})
    if not args.summary:
        return rows
    out = []
    for sp in ("sue", "suap"):
        r = [x for x in rows if sp in x["sportelli"]]
        if not r:
            continue
        done = [x for x in r if x["covered"]]
        out.append(f"{sp.upper()}: {len(r)} distinct sections, {len(done)} in the dictionary; "
                   f"{sum(x['uses'] for x in done)} of {sum(x['uses'] for x in r)} uses "
                   f"({100 * sum(x['uses'] for x in done) // max(1, sum(x['uses'] for x in r))}%)")
    return out


def stored_fingerprints(name, cache=None):
    """Each section of a stored crawl and its fingerprint."""
    cache = cache if cache is not None else {}
    out = {}
    for s in json.loads((SECTIONS / f"{name}.json").read_text())["sections"]:
        if s.get("skipped") or s.get("error"):
            continue
        if s.get("same_as"):
            out[s["section"]] = s.get("fingerprint") or fingerprint(resolve(s["same_as"], cache)["fields"])
        else:
            out[s["section"]] = s.get("fingerprint") or fingerprint(s["fields"])
    return out


def cmd_drift(args):
    """A check batch (`crawl:check:*` keys) against the stored crawls, into drift.json: the
    pratiche whose sections changed, which gen.py and run.py then refuse."""
    data = json.loads(Path(args.export).read_text())
    path = CAT / "drift.json"
    drift = json.loads(path.read_text()) if path.exists() else {"pratiche": {}}
    out, cache = [], {}
    for key, r in sorted(data.items()):
        if not key.startswith("crawl:check:"):
            continue
        r = r if isinstance(r, dict) else json.loads(r)
        name = r["name"]
        if r.get("error"):
            out.append(f"{name}: not checked ({r['error']})")
            continue
        if not (SECTIONS / f"{name}.json").exists():
            out.append(f"{name}: no stored crawl to compare")
            continue
        old = stored_fingerprints(name, cache)
        now = {s["section"]: s.get("fingerprint") for s in r["sections"] if not s.get("skipped") and not s.get("error")}
        diff = sorted(f"gone: {n}" for n in old if n not in now) + sorted(f"new: {n}" for n in now if n not in old)
        diff += sorted(f"changed: {n}" for n in now if n in old and now[n] != old[n])
        drift["pratiche"][name] = {"checked": r["crawled"][:10], "differences": diff}
        out.append(f"{name}: " + ("same as the stored crawl" if not diff else "; ".join(diff)))
    path.write_text(json.dumps(drift, ensure_ascii=False, indent=1) + "\n")
    return out


def drifted(name):
    """The differences the last check found in a pratica's crawl, or []."""
    path = CAT / "drift.json"
    if not path.exists():
        return []
    return json.loads(path.read_text())["pratiche"].get(name, {}).get("differences", [])


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = ap.add_subparsers(dest="cmd", required=True)
    p = sub.add_parser("ops")
    p.add_argument("sportello", choices=["sue", "suap"])
    p.add_argument("--sectors", help="SUAP sector indices in suap-wizard.json, comma-separated")
    p.add_argument("--common", action="store_true", help="the common operations, under the first sector")
    p.add_argument("--all", action="store_true", help="also the pratiche already crawled")
    sub.add_parser("known")
    p = sub.add_parser("import")
    p.add_argument("export")
    p = sub.add_parser("count")
    p.add_argument("--summary", action="store_true")
    p = sub.add_parser("drift")
    p.add_argument("export")
    args = ap.parse_args()
    res = globals()["cmd_" + args.cmd](args)
    if isinstance(res, list) and res and isinstance(res[0], str):
        print("\n".join(res))
    else:
        print(json.dumps(res, ensure_ascii=False, indent=1 if args.cmd == "count" else None))


if __name__ == "__main__":
    main()
