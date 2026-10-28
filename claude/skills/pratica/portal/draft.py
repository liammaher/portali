#!/usr/bin/env python3
"""Draft dictionary entries for crawled sections the dictionary doesn't have yet, from the
national schema of the same form (InfoCamere's Moduli PA, flattened by
rehearsals/cila-fittizia/schema/flatten.py).

  draft.py CRAWL.json SCHEMA-FIELDS.json [SCHEMA-FIELDS.json ...] [--section NAME ...] [--out DRAFT.yaml]

A SUAP pratica can join several forms (a restaurant's SCIA and the Reg. 852 notifica): give
them all, and each section takes the form group that fits it best.

The portal builds its sections from the schema's entities, in the schema's order, so the
crawled boxes (in page order, each choice followed by what it opens) and the schema's leaves
line up: they are aligned as two sequences, scored by kind (a tick is a boolean, a mq box a
number, a date a date) and by the words a label shares with a leaf's name. Each aligned box
becomes a dictionary field whose value is a pre-flight question named after the leaf
(`superficie_complessiva`), typed from the schema (number, date, yes/no, the dropdown's
options); two radios of one group become one yes/no question, the first ticked on yes. A box
no leaf aligns with is listed for a person to write.

A draft is a proposal: a person checks it (the person-or-business values, which questions
the case pins in `supports`, the declarations that are the person's) before it joins
dictionary/fnsuap.yaml, and a run on the portal with made-up data proves it.
"""

import argparse
import json
import re
import sys
import unicodedata
from pathlib import Path

import yaml

sys.path.insert(0, str(Path(__file__).resolve().parent))
import catalogue  # noqa: E402  (what the dictionary fills)
import gen  # noqa: E402  (norm, the dictionary lookup)

# Statements in the person's name, never ticked for them; the second half is crawl.js's PERSONAL.
DECLARATION = re.compile(r"non sussistono nei propri confronti|consapevol|dichiara di essere|"
                         r"presa visione|privacy|autorizz|sottoscritt", re.I)
STOP = {"di", "del", "della", "dei", "il", "la", "le", "lo", "e", "o", "a", "in", "per", "con", "che", "n", "nr",
        "check", "si", "no", "ctype", "stype", "voc", "blocco", "al", "allo", "alla", "ai", "agli", "alle", "dal",
        "dalla", "nel", "nella", "ed", "od", "un", "uno", "una", "gli", "sul", "sulla", "presso", "ecc", "all",
        "gg", "mm", "aa", "aaaa"}
ALIASES = {"mq": "superficie", "prot": "protocollo", "n.": "numero", "cap": "cap", "civico": "civico",
           "rilasciata": "ente", "atto": "atto", "decreto": "atto"}


def words(s):
    s = unicodedata.normalize("NFKD", s or "").encode("ascii", "ignore").decode().lower()
    s = re.sub(r"([a-z])([A-Z])", r"\1 \2", s)
    out = set()
    for w in re.split(r"[^a-z0-9]+", s):
        if len(w) > 1 and w not in STOP:
            out.add(ALIASES.get(w, w))
            if len(w) > 6:
                out.add(w[:6])  # superficie/superfice, qualifica/qualificazione
    return out


def leaf_kind(f):
    t = (f.get("type") or "").lower()
    if t == "boolean":
        return "tick"
    if any(k in t for k in ("double", "decimal", "integer", "int")):
        return "number"
    if "ggmmaaaa" in t or "date" in t:
        return "date"
    if f.get("options") or t.endswith("_voc") or t == "codetype":
        return "select"
    return "text"


COUNTER = re.compile(r"^\d+ Caratteri restanti$", re.I)
RARITY = {}  # word → how many leaves of the section's group have it (draft_section sets it)


def box_kind(b):
    if b["kind"] in ("radio", "checkbox"):
        return "tick"
    if b["kind"] == "date" or b["id"].endswith("calendar_input"):
        return "date"
    if b["id"].endswith(":real") or b["id"].endswith(":number"):
        return "number"
    return {"select": "select", "file": "file"}.get(b["kind"], "text")


def boxes(section):
    """The section's boxes in page order: each choice followed by what it opens, and each of
    those by what it opens in turn."""
    out, seen = [], set()

    def add(f, by=None):
        if f["kind"] == "message" or f["id"] in seen or f.get("prefilled"):
            return
        seen.add(f["id"])
        out.append({**f, "opened_by": by} if by else f)

    def opened(node):
        for x in node.get("reveals", []):
            add(x, node["id"])
            for child in node.get("then", []):
                if child["id"] == x["id"]:
                    opened(child)

    # Base boxes by row; what a choice opens goes where its rows are, before the base boxes
    # that come after it on the page.
    base = [f for f in section.get("fields", []) if f["kind"] != "message"]
    row = lambda f: (gen.pos(f["id"]) or (0, 0))[0]
    pending = []
    for f in base:
        while pending and pending[0][0] <= row(f):
            for x in pending.pop(0)[1]:
                add(x, x.get("opened_by"))
        add(f)
        for n in section.get("branches", []):
            if n["id"] == f["id"]:
                mark = len(out)
                opened(n)
                more = out[mark:]
                del out[mark:]
                for x in more:
                    seen.discard(x["id"])
                more = [{k: v for k, v in x.items()} for x in more]
                if more:
                    pending.append((row(more[0]), more))
                    pending.sort(key=lambda p: p[0])
    for _, xs in pending:
        for x in xs:
            add(x, x.get("opened_by"))
    return out


def box_words(b):
    """A box's words: its label's, and its context's when the label alone says little ("mq",
    "del"); a full label of its own isn't mixed with the cell beside it."""
    label = "" if COUNTER.match(b.get("label", "")) else b.get("label", "")
    own = words(label)
    return own if len(own - {w[:6] for w in own if len(w) > 6} - {w for w in own if len(w) <= 6 and any(o != w and o.startswith(w) for o in own)}) >= 3 \
        else own | words(b.get("context", ""))


def leaf_words(leaf):
    """A leaf's words, without the form's own name: every leaf of the restaurant's form would
    share "somministrazione" with a box about the surface of somministrazione."""
    name = leaf["path"].rsplit("/", 1)[-1]
    return words(name + " " + (leaf.get("note") or "")) - words(leaf["path"].split("/", 1)[0])


def score(b, leaf):
    bk, lk = box_kind(b), leaf_kind(leaf)
    if bk == "file":
        return -1
    kind = 1.0 if bk == lk or (bk == "text" and lk in ("text", "number")) or (bk == "number" and lk == "text") else -0.5
    name = leaf["path"].rsplit("/", 1)[-1]
    shared = box_words(b) & leaf_words(leaf)
    # A word's prefix counts once with it; a word many leaves share counts less ("prodotti" is
    # in a dozen of the 852 list's, "carne" in one).
    shared = {w for w in shared if not any(o != w and o.startswith(w) for o in shared)}
    # A prefix alone ("dettag" of dettaglianti and dettaglio) is half a match.
    full = box_words(b) & leaf_words(leaf)
    half = lambda w: 0.5 if len(w) == 6 and not any(o != w and o.startswith(w) for o in full) else 1.0
    return kind + sum(1.5 * half(w) * min(1.0, 2 / RARITY.get(w, 1)) for w in shared)


def kin(b, leaf):
    """Whether a box and a leaf may pair: a word in common, or a box whose label says nothing
    of its own ("del", "n.", a bare tick) placed by its neighbours."""
    return bool(box_words(b) & leaf_words(leaf)) or len(words(b.get("label", ""))) <= 1


def align(bs, leaves, gap=-0.3):
    """Needleman-Wunsch over the two lists: [(box, leaf or None)]."""
    n, m = len(bs), len(leaves)
    S = [[0.0] * (m + 1) for _ in range(n + 1)]
    for i in range(1, n + 1):
        S[i][0] = i * gap
    for j in range(1, m + 1):
        S[0][j] = j * gap * 0.2  # schema leaves the portal doesn't show cost little
    for i in range(1, n + 1):
        for j in range(1, m + 1):
            S[i][j] = max(S[i - 1][j - 1] + score(bs[i - 1], leaves[j - 1]), S[i - 1][j] + gap, S[i][j - 1] + gap * 0.2)
    out, i, j = [], n, m
    while i > 0 and j > 0:
        # On a tie a later leaf stays unused, so a box takes the nearest leaf after the one
        # before it ("del" right after "prot./n.").
        if S[i][j] == S[i][j - 1] + gap * 0.2:
            j -= 1
        elif S[i][j] == S[i - 1][j - 1] + score(bs[i - 1], leaves[j - 1]):
            out.append((bs[i - 1], leaves[j - 1] if score(bs[i - 1], leaves[j - 1]) > 0.5 and kin(bs[i - 1], leaves[j - 1]) else None))
            i, j = i - 1, j - 1
        else:
            out.append((bs[i - 1], None))
            i -= 1
    out += [(bs[k], None) for k in range(i - 1, -1, -1)]
    out.reverse()
    # A box the alignment gave a leaf sharing no word with it takes a nearby unused leaf that
    # does (the portal asks Cognome before Nome, the schema nome before cognome).
    used = {id(l) for _, l in out if l}
    for k, (b, l) in enumerate(out):
        bw = words(b.get("label", "") + " " + b.get("context", ""))
        if l is None or bw & words(l["path"].rsplit("/", 1)[-1]):
            continue
        at = next((x for x, y in enumerate(leaves) if y is l), 0)
        for y in leaves[max(0, at - 3):at + 4]:
            if id(y) not in used and bw & words(y["path"].rsplit("/", 1)[-1]) and score(b, y) > 0.5:
                used.discard(id(l)); used.add(id(y)); out[k] = (b, y)
                break
    return out


# Groups the portal fills in sections of its own (the Scheda anagrafica, the attachments
# screen): another section takes one only when its title names it.
SHARED_GROUPS = {"scheda_anagrafica", "tabellaAllegati"}


def best_group(bs, schema, title=""):
    """The schema group (second level of the form) whose leaves best fit the section, with
    the section's title as a hint ("notifica di subingresso" is the notifica's subingresso)."""
    groups = {}
    for f in schema:
        parts = f["path"].split("/")
        if len(parts) > 2 and f["type"] != "group":
            groups.setdefault("/".join(parts[:2]), []).append(f)
    tw = words(title)

    def fit(kv):
        form, group = kv[0].split("/", 1)
        named = tw & (words(group) - words(form) | words(form))
        if group in SHARED_GROUPS and not tw & words(group):
            return -1e9
        return sum(max([score(b, l) for l in kv[1]] or [0]) for b in bs) + 3 * len(named)
    best = max(groups.items(), key=fit)
    return best[0], best[1]


def short(path):
    leaf = path.rsplit("/", 1)[-1]
    return re.sub(r"_check$", "", leaf)


def draft_section(section, schema):
    bs = boxes(section)
    RARITY.clear()
    group, leaves = best_group(bs, schema, section["section"])
    for leaf in leaves:
        for w in leaf_words(leaf):
            RARITY[w] = RARITY.get(w, 0) + 1
    pairs = align(bs, leaves)
    # A box left with no leaf, or one it shares no word with, takes the best unused leaf of the
    # forms that it does share one with, wherever it sits ("mq" is the surface, far down).
    # A form can repeat a group under another name (the 852 notifica's avvio_attivita and
    # avvio_attivita_section): a leaf's name taken is taken in both.
    name = lambda leaf: leaf["path"].rsplit("/", 1)[-1]
    used = {name(leaf) for _, leaf in pairs if leaf}
    for k, (b, leaf) in enumerate(pairs):
        bw = box_words(b)
        if not bw or (leaf and bw & leaf_words(leaf)):
            continue
        near = [y for y in schema if y["path"].split("/", 1)[0] == group.split("/", 1)[0] and y.get("type") != "group"
                and name(y) not in used and bw & leaf_words(y) and score(b, y) >= 2]  # within the section's form
        if near:
            y = max(near, key=lambda y: (score(b, y), y in leaves))
            used.discard(leaf and name(leaf)), used.add(name(y))
            pairs[k] = (b, y)
    # Then a box takes a leaf that shares clearly more words with it than its own, when that
    # leaf is free, or swaps with the box holding it when both fit better ("COMMERCIO AL
    # DETTAGLIO" had taken commercio_ingrosso). Repeated labels score alike and stay in order.
    sc = lambda b, leaf: score(b, leaf) if leaf else 0
    for k in range(len(pairs)):
        b, leaf = pairs[k]
        holder = {name(l): i for i, (_, l) in enumerate(pairs) if l}
        best = max(leaves, key=lambda y: sc(b, y), default=None)
        if best is None or best is leaf or sc(b, best) < sc(b, leaf) + 1.4:
            continue
        h = holder.get(name(best))
        if h is None:
            pairs[k] = (b, best)
        elif sc(b, best) + sc(pairs[h][0], leaf) > sc(b, leaf) + sc(pairs[h][0], best) + 1:
            pairs[k], pairs[h] = (b, best), (pairs[h][0], leaf if leaf and sc(pairs[h][0], leaf) > 0.5 else None)
    fields, asks, unmatched, theirs = [], {}, [], []
    # Radios go together when their leaves do (the portal names each radio apart):
    # centro_commerciale_si and _no under in_centro_commerciale.
    parent = lambda leaf: "/".join(p for p in leaf["path"].split("/")[:-1] if not p.startswith("["))
    by_group, leaf_of = {}, {b["id"]: leaf for b, leaf in pairs if leaf}
    for b, leaf in pairs:
        if b["kind"] == "radio" and leaf:
            b["group"] = parent(leaf)
            by_group.setdefault(b["group"], []).append(b)
    def name_of(leaf):
        # A leaf's question name; two leaves ending alike ("altro_check", "altro") keep apart.
        n = short(leaf["path"])
        if n in asks and asks[n]["schema"] != leaf["path"]:
            n = leaf["path"].rsplit("/", 1)[-1]
            if n in asks:
                n += "_testo"
        return n

    when_of = {}  # a choice's condition, for the boxes it opens

    tick_ids = {gen.norm(x["label"]): x["id"] for x, _ in pairs if x["kind"] in ("radio", "checkbox")}

    def inherit(field, b):
        # Opened by a choice, or always shown but labelled by one ("di", beside "Svolta
        # congiuntamente all'attività"): it goes with that choice.
        by = b.get("opened_by") or tick_ids.get(gen.norm(b.get("context", "")))
        w = when_of.get(by)
        if w:
            field["when"] = (w + " " + field["when"]) if field.get("when") else w
        if field.get("when"):
            when_of[b["id"]] = field["when"]
        return field

    seen_labels = {}

    def counted(field, b):
        # A label the section repeats ("mq", "rilasciata da") gets its place among them.
        key = (box_kind(b), gen.norm(field["label"]))
        seen_labels[key] = seen_labels.get(key, 0) + 1
        if seen_labels[key] > 1:
            field["nth"] = seen_labels[key]
        return field

    for b, leaf in pairs:
        label = b["label"] if len(b["label"]) < 70 else b["label"][:60].rsplit(" ", 1)[0]
        if COUNTER.match(label) and b.get("context"):
            label = b["context"][:60]  # a textarea's "500 Caratteri restanti" (gen.py reads context too)
        if b["kind"] == "checkbox" and DECLARATION.search(b["label"]):
            theirs.append(label)  # a statement in the person's name: never ticked for them
            continue
        if leaf is None and b["kind"] == "file":
            # An upload the schema lists only among the attachments: the person's file.
            key = "allegato_" + re.sub(r"[^a-z0-9]+", "_", gen.norm(section["section"]))[:40].strip("_")
            asks[key] = {"type": "file", "question": (b.get("context") or b["label"])[:120], "schema": None}
            fields.append(inherit({"kind": "file", "label": "Tipologie ammesse", "value": f"{{{{ask.{key}}}}}"}, b))
            continue
        if leaf is None:
            unmatched.append(b)
            fields.append({"kind": b["kind"], "label": label, "note": "no schema leaf aligned: a person writes this one"})
            continue
        name = name_of(leaf)
        k = box_kind(b)
        if k == "tick":
            siblings = by_group.get(b.get("group"), []) if b["kind"] == "radio" else []
            if len(siblings) == 2:
                # One question for the pair, named after what the schema groups them under
                # (in_centro_commerciale), the first radio ticked on yes.
                first = siblings[0]
                lead = leaf_of.get(first["id"]) or leaf
                parent = [p for p in lead["path"].split("/")[:-1] if not p.startswith("[")][-1]
                key = re.sub(r"_(si|no)$", "", parent)
                asks.setdefault(key, {"type": "yes_no", "question": f"{parent.replace('_', ' ').capitalize()}? (yes: {first['label'][:40]})",
                                      "schema": lead["path"]})
                when = f"{{{{ask.{key}}}}}" if b is first else f"{{{{ask.{key}|not}}}}"
            else:
                asks[name] = {"type": "yes_no", "question": f"{b['label'][:100]}?", "schema": leaf["path"]}
                when = f"{{{{ask.{name}}}}}"
            fields.append(counted(inherit({"kind": "tick", "label": label, "when": when}, b), b))
            continue
        q = b.get("context") or b["label"]
        ask = {"type": {"number": "number", "date": "date", "select": "choice"}.get(k, "text"),
               "question": f"{q[:90]}{'' if q == b['label'] else ': ' + b['label'][:40]}", "schema": leaf["path"]}
        if k == "select" and isinstance(b.get("options"), list):
            ask["options"] = b["options"]
        asks[name] = ask
        value = f"{{{{ask.{name}{'|date_it' if k == 'date' else ''}}}}}"
        fields.append(counted(inherit({"kind": "textarea" if b["kind"] == "textarea" else ("date" if k == "date" else b["kind"] if b["kind"] in ("select", "lookup") else "text"),
                                       "label": label, "value": value}, b), b))
    entry = {"section": section["section"], "schema": group, **({"theirs": theirs} if theirs else {}), "fields": fields}
    return entry, asks, len(bs), len(unmatched)


def keep_apart(entry, asks, taken):
    """An entry whose question names another section already uses for another schema leaf
    ("da_stagionale" for the season's start and for bread): renamed, with its fields."""
    text, out = json.dumps(entry, ensure_ascii=False), {}
    for k, a in asks.items():
        new = k
        while new in taken and taken[new].get("schema") != a.get("schema"):
            new = f"{k}_{int(new.rsplit('_', 1)[-1]) + 1 if new != k and new.rsplit('_', 1)[-1].isdigit() else 2}"
        if new != k:
            text = re.sub(r"\{\{ask\." + re.escape(k) + r"(?=[|}])", "{{ask." + new, text)
        out[new] = a
    return json.loads(text), out


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("crawl", type=Path)
    ap.add_argument("schema", type=Path, nargs="+")
    ap.add_argument("--section", action="append", default=[])
    ap.add_argument("--out", type=Path)
    a = ap.parse_args()
    crawl = json.loads(a.crawl.read_text())
    schema = [leaf for p in a.schema for leaf in json.loads(p.read_text())]
    dictionary = gen.load_yaml(gen.HERE / "dictionary" / "fnsuap.yaml")
    entries, asks, report = [], {}, []
    for s in crawl["sections"]:
        if s.get("skipped") or s.get("error") or s.get("same_as") or gen.DECLARATIONS.search(s["section"]):
            continue  # a batch's same_as section is drafted where it was crawled
        if a.section and not any(gen.norm(s["section"]).startswith(gen.norm(x)) for x in a.section):
            continue
        if not a.section and catalogue.covered(s, dictionary):
            continue  # the dictionary fills it
        e, q, n, missed = draft_section(s, schema)
        e, q = keep_apart(e, q, asks)
        entries.append(e)
        asks.update(q)
        report.append(f"{s['section'][:60]}: {n} boxes, {n - missed} drafted from {e['schema'].split('/')[-1]}, {missed} for a person")
    out = yaml.dump({"asks": asks, "sections": entries}, allow_unicode=True, sort_keys=False, width=140)
    if a.out:
        a.out.write_text("# Drafted by draft.py from the national schema: check before it joins fnsuap.yaml.\n" + out)
    else:
        print(out)
    print("\n".join(report), file=sys.stderr)


if __name__ == "__main__":
    main()
