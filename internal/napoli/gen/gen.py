#!/usr/bin/env python3
"""Builds ../data/stradario.json from the Comune di Napoli's two stradari.

  gen.py <stradario-per-municipalita.pdf> <stradario-cosap-2003.pdf> <out.json>

1. Stradario "ordinato per municipalità" (aggiornamento al 22/12/2025), from
   https://www.comune.napoli.it/stradario : one row per street and quartiere,
   with the municipalità. It has no house numbers.
2. "Stradario COSAP" (2003), https://trasparenza.comune.napoli.it/flex/files/1/f/1/D.f5e6878d6c0cf0699c29/6__stradario.pdf :
   its note column gives civico ranges for streets that cross quartieri
   ("da 1 a 179 (consecutiva)"). It is old, so a range is kept only when every
   one of its rows parses and its quartieri are among the 2025 ones.

Needs PyMuPDF (the fill pilot already does). The output is deterministic.
"""
import collections
import json
import re
import sys

import fitz

# The 2025 PDF's own mistakes, by street code: the municipalità or quartiere
# column is missing, doubled or overflowed by the name.
FIXES = {
    1807: dict(name="VIA RAVASCHIERI TERESA", quartiere="Chiaia", mun=1),
    5088: dict(quartiere="", mun=1),  # quartiere column holds a stray backslash
}
QUARTIERE_ALIASES = {
    "S. Ferdinando": "S.Ferdinando", "San Ferdinando": "S.Ferdinando",
    "S. Carlo all'Arena": "S.Carlo Arena", "S. Carlo all’Arena": "S.Carlo Arena",
    "Pnticelli": "Ponticelli", "S.Giov a Ted": "S.Giov.a Ted",
    "S.Giovanni a Teduccio": "S.Giov.a Ted", "S.Pietro a Patierno": "S.Pietro a Pat.",
}


def words_by_row(page, ymin, ytol):
    rows = []
    for w in sorted(page.get_text("words"), key=lambda w: (w[1], w[0])):
        if w[1] < ymin or w[1] > 570:
            continue
        for r in rows:
            if abs(r[0] - w[1]) < ytol:
                r[1].append(w)
                break
        else:
            rows.append((w[1], [w]))
    return [sorted(ws, key=lambda w: w[0]) for _, ws in rows]


def parse_2025(path):
    out = []
    for page in fitz.open(path):
        words = [w for w in page.get_text("words") if 36 < w[1] < 570]
        # A row starts where its COD is. The name may begin a line above or below
        # it (wrapped cells), so a word belongs to the row whose COD is nearest
        # above it, unless a row starts closer below it.
        anchors = sorted(w[1] for w in words if w[0] < 52)
        if not anchors:
            continue
        rows = [[] for _ in anchors]
        for w in words:
            j = min(range(len(anchors)), key=lambda i: abs(anchors[i] - w[1]))
            tol = 16 if w[0] >= 720 else 9
            if abs(anchors[j] - w[1]) > tol:
                raise SystemExit("orphan word %r on page %d" % (w[4], page.number + 1))
            rows[j].append(w)
        for ws in rows:
            ws.sort(key=lambda w: w[0])
            col = lambda lo, hi: " ".join(w[4] for w in ws if lo <= w[0] < hi)
            out.append(dict(cod=int(col(0, 52)), name=col(72, 620), quartiere=col(620, 720), mun=col(720, 900)))
    return out


def street_segments(rows):
    """{cod: (name, sorted [(quartiere, mun)])}, with the PDF's slips mended."""
    q2m = collections.defaultdict(collections.Counter)
    for r in rows:
        q = QUARTIERE_ALIASES.get(r["quartiere"], r["quartiere"])
        if q and r["mun"].isdigit():
            q2m[q][int(r["mun"])] += 1
    # A quartiere is in one municipalità; the few slips are outvoted.
    quartiere_mun = {q: c.most_common(1)[0][0] for q, c in q2m.items() if sum(c.values()) >= 5}
    streets = {}
    for r in rows:
        fix = FIXES.get(r["cod"], {})
        name = fix.get("name", r["name"])
        q = QUARTIERE_ALIASES.get(fix.get("quartiere", r["quartiere"]), fix.get("quartiere", r["quartiere"]))
        muns = {int(m) for m in re.findall(r"\d+", r["mun"])}
        if "mun" in fix:
            muns = {fix["mun"]}
        if q and q in quartiere_mun and (len(muns) != 1 or quartiere_mun[q] not in muns):
            muns = {quartiere_mun[q]}  # blank or doubled MUN column
        if q and q not in quartiere_mun:
            raise SystemExit("unknown quartiere %r (cod %s)" % (q, r["cod"]))
        if len(muns) != 1:
            raise SystemExit("no municipalità for cod %s %s" % (r["cod"], name))
        if q and quartiere_mun[q] != next(iter(muns)):
            raise SystemExit("quartiere %s and municipalità disagree (cod %s)" % (q, r["cod"]))
        s = streets.setdefault(r["cod"], (name, set()))
        if s[0] != name:
            # One row may lack the street type (3979 SEDIL CAPUANO): keep the fuller name.
            short, full = sorted((s[0], name), key=len)
            if not full.endswith(" " + short):
                raise SystemExit("cod %s has two names" % r["cod"])
            streets[r["cod"]] = (full, s[1])
        s[1].add((q, next(iter(muns))))
    return {c: (n, sorted(segs)) for c, (n, segs) in streets.items()}, quartiere_mun


def norm_key(name):
    return " ".join(re.sub(r"[^A-Z0-9]+", " ", name.upper()).split())


CLAUSE = re.compile(
    r"(?:(?:da|dal)\s+)?(\d+)(?:/\w+)?\s+(?:a|al)\s+(\d+)(?:/(\w+))?(?:\s*\((dispari|pari|consecutiv[ae])\))?"
    r"|solo\s+civico\s+(\d+)"
)
PARITY = {"dispari": 1, "pari": 2}


def parse_note(note):
    """The civico ranges of a note as [lo, hi, parity] (parity 0 any, 1 odd,
    2 even), plus the letter the upper bound ends with ("52/A" gives "A"), or
    None when any of it is not understood."""
    note = note.lower().replace("’", "'")
    spans, ranges = [], []
    for m in CLAUSE.finditer(note):
        spans.append(m.span())
        if m.group(5):
            ranges.append([int(m.group(5)), int(m.group(5)), 0])
        else:
            r = [int(m.group(1)), int(m.group(2)), PARITY.get(m.group(4), 0)]
            if m.group(3):
                r.append(m.group(3).upper())  # civici after 52/A, such as 52/B, are outside
            ranges.append(r)
    rest = note
    for a, b in reversed(spans):
        rest = rest[:a] + " " + rest[b:]
    rest = re.sub(r"\(consecutiv[ae]\)", " ", rest)
    rest = re.sub(r"\b(gi[àa]|detta anche)\b.*", " ", rest)  # a former name, nothing to read
    rest = re.sub(r"[-\s]|\be\b", "", rest)
    if not ranges or rest:
        return None
    # A pari/dispari on only some clauses of a list is ambiguous about the rest.
    marked = {r[2] for r in ranges}
    if len(ranges) > 1 and 0 in marked and marked != {0}:
        return None
    # Clauses with no pari/dispari that overlap ("da 1 a 61 e da 4 a 626") are
    # an odd run and an even run written without saying so.
    if len(ranges) > 1 and marked == {0}:
        spans = sorted((r[0], r[1]) for r in ranges)
        if any(b[0] <= a[1] for a, b in zip(spans, spans[1:])):
            return None
    return ranges


def parse_2003(path, labels):
    out = []
    for page in fitz.open(path):
        for ws in words_by_row(page, 105, 4):
            q = ""
            for k in range(4, 0, -1):
                if len(ws) > k and " ".join(w[4] for w in ws[-k:]) in labels:
                    q = " ".join(w[4] for w in ws[-k:])
                    ws = ws[:-k]
                    break
            if not q:
                continue
            name = " ".join(w[4] for w in ws if w[0] < 440)
            note = " ".join(w[4] for w in ws if w[0] >= 465)
            out.append((norm_key(name), QUARTIERE_ALIASES.get(q, q), note.strip()))
    return out


def main(pdf2025, pdf2003, dest):
    streets, quartiere_mun = street_segments(parse_2025(pdf2025))
    labels = set(quartiere_mun) | set(QUARTIERE_ALIASES) | {"Zona Mare--/A", "Zona Mare--/B", "Zona Mare--/C", "Zona Mare--/D"}
    old = collections.defaultdict(list)
    for key, q, note in parse_2003(pdf2003, labels):
        old[key].append((q, note))
    out, with_ranges, ambiguous = [], 0, 0
    for cod in sorted(streets):
        name, segs = streets[cod]
        entry = {"c": cod, "n": name, "s": [[q, m] for q, m in segs]}
        if len({m for _, m in segs}) > 1:
            ambiguous += 1
        rows = old.get(norm_key(name))
        # Every quartiere of the street must have its range: one without would be a
        # part of the street whose civici are unknown, and a civico in the others'
        # ranges must not settle it. A quartiere only in the 2003 stradario is a
        # sign the two are not the same street.
        if rows and len({q for q, _ in rows}) > 1 and {q for q, _ in rows} == {q for q, _ in segs}:
            per_q, ok = collections.defaultdict(list), True
            for q, note in rows:
                rs = parse_note(note)
                if rs is None:
                    ok = False
                    break
                per_q[q] += rs
            if ok:
                entry["r"] = [[q, per_q[q]] for q in sorted(per_q)]
                with_ranges += 1
        out.append(entry)
    doc = {
        "stradario": "Comune di Napoli, stradario ordinato per municipalità, aggiornamento al 22/12/2025",
        "civici": "Comune di Napoli, stradario COSAP 2003 (ranges only where every row was understood)",
        "streets": out,
    }
    with open(dest, "w", encoding="utf-8") as f:
        json.dump(doc, f, ensure_ascii=False, separators=(",", ":"))
        f.write("\n")
    print("%d streets, %d across municipalità, %d with civico ranges" % (len(out), ambiguous, with_ranges), file=sys.stderr)
    for cod in sorted(streets):
        name, segs = streets[cod]
        e = next(x for x in out if x["c"] == cod)
        if len({m for _, m in segs}) > 1 and "r" not in e:
            print("  no ranges: %s %s" % (name, sorted({m for _, m in segs})), file=sys.stderr)


if __name__ == "__main__":
    if len(sys.argv) != 4:
        raise SystemExit(__doc__)
    main(*sys.argv[1:])
