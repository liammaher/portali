#!/usr/bin/env python3
"""Write a pratica's steps file from what was crawled, instead of walking it by hand.

  gen.py RECIPE.yaml [--out STEPS.yaml] [--check STEPS.yaml]

RECIPE.yaml names the pratica: the comune's catalogue (`catalogue/<comune>/sue-wizard.json`,
which sections the operation and its scelte make), the section crawl of a draft of it
(`catalogue/<comune>/sections/<name>.json`, crawl.js: each field's id, label and kind, what
each choice opens and which ids it shifts), and its own few lines: title, the person's
steps, the cases it supports, the screens after the form. What goes in each field comes
from the dictionary (`dictionary/fnsuap.yaml`): per section, by label, the vault field or
the pre-flight question, the same for every pratica that has the section.

Each dictionary field is found in the crawled section by kind and label (its first texts,
as inpage.js reads them) and gets its id in the state the choices before it leave. Where
the crawl saw that state (a choice and what it opened) the id is the crawl's; where it
tried two choices apart, their offsets add up (rN cK pM: a choice that adds rows shifts
every later row and position by the same amount; on the CILA's a) this gives the 21 ids
the hand walk found). Fields in a "+" block go by label (`anchor`, `block`): inpage.js finds
them on the live page.

Sections are matched without their quadro letter (the CIL's f) is the CILA's h)), and the
first dictionary entry that fits the crawled section wins. A field under a `when` lives in
its own branch of the section; a field outside it that such a choice moves goes by label,
and so does a box whose own tick renumbers it (it opens rows above itself). An attachments
slot that takes only signed files (.p7m) is left to the person.

A recipe's `operation` may be a list (a CILA with its autorizzazione paesaggistica): the
pratica is the union of each operation's sections with the recipe's `scelte`, in the order
of the combination's own crawl, which must hold just those (a section both have under two
names, the same fields, is one). Every scelta named is ticked.

It stops, writing nothing, on a section the dictionary doesn't have, a dictionary field
the crawl doesn't show, or a field the section shows that no dictionary entry fills or
leaves (`leave:`), so a pratica is generated whole or not at all. --check compares the
result with a hand-written steps file, field by field.

A recipe's `rules:` names the library's rules table that decides whether a case is this
pratica (`rules/<table>.yaml`), the facts the recipe fixes (`facts:`, a value or a template)
and the other pratiche it files with it (`handles:`). Every other fact is the answer of the
ask of its name, written from the table's question where the pratica has none (yes/no facts
in one checklist); run.py preflight then refuses a case the table gives to another pratica.
"""

import argparse
import copy
import json
import re
import sys
import unicodedata
from pathlib import Path

import yaml

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import rules  # noqa: E402
DECLARATIONS = re.compile(
    r"consapevolezza|informativa sulla privacy|requisiti di o|altre dichiarazioni|"
    r"dichiarazioni ai sensi dell'art\. 6|rispetto sulle tasse|art\. ?75", re.I)
ID = re.compile(r"^r(\d+)c(\d+)p(\d+)reg(.*)$")
# inpage.js matches a field's kind as it reads the page: a date or a textarea is text.
PAGE_KIND = {"date": "text", "textarea": "text", "number": "text"}


class GenError(Exception):
    pass


FATTI = "Quali di queste cose valgono per l'attività? Spunta quelle vere, anche nessuna."


def rules_block(spec, asks, groups):
    """A recipe's `rules:` as its steps file carries it: every fact of the table, the
    recipe's value or `{{ask.<fact>}}`, adding the ask from the table's question when the
    pratica has none. Changes asks and groups in place."""
    try:
        t = rules.table(spec["table"])
    except ValueError as e:
        raise GenError(str(e))
    facts = dict(spec.get("facts", {}))
    for name in sorted(set(facts) - set(t["facts"])):
        raise GenError(f"rules: table {spec['table']} has no fact {name}")
    for name, v in facts.items():
        if not (isinstance(v, str) and "{{" in v) and rules.fact_value(t["facts"][name], v) is None:
            raise GenError(f"rules: {v!r} isn't a value of the fact {name}")
    out = {}
    for name, fact in t["facts"].items():
        if name in facts:
            out[name] = facts[name]
            continue
        a, kind = asks.get(name), fact["type"]
        if a is None:
            if kind == "yes_no":
                a = {"group": "fatti", "type": "yes_no", "item": fact["question"]}
                groups.setdefault("fatti", {"question": FATTI})
            else:
                a = {"type": kind, "question": fact["question"]}
                if kind == "choice":
                    a |= {"options": list(fact["values"]), "labels": fact["values"]}
            if fact.get("how"):
                a["how"] = fact["how"]
            asks[name] = a
        elif a.get("type") != kind or (kind == "choice" and set(map(str, a.get("options", []))) - set(fact["values"])):
            raise GenError(f"rules: the fact {name} is also asks.{name}, a {a.get('type')}: give its value in the "
                           "recipe's rules.facts")
        out[name] = "{{ask.%s}}" % name
    return {"table": spec["table"], "facts": out, **({"handles": spec["handles"]} if spec.get("handles") else {})}


def norm(s):
    s = unicodedata.normalize("NFKD", s or "")
    return re.sub(r"\s+", " ", "".join(c for c in s if ord(c) < 128)).strip().lower()


def pos(fid):
    m = ID.match(fid)
    return (int(m[1]), int(m[3])) if m else None


def shift(fid, dr, dp):
    m = ID.match(fid)
    return f"r{int(m[1]) + dr}c{m[2]}p{int(m[3]) + dp}reg{m[4]}" if m else fid


def matches(f, kind, label, inside=False):
    a, b = PAGE_KIND.get(f["kind"], f["kind"]), PAGE_KIND.get(kind, kind)
    if b == "tick":  # the same choice is a radio in one pratica, a checkbox in another
        b = a if a in ("radio", "checkbox") else b
    # A lookup box can be typed into as text (by id: inpage.js only looks lookups up by kind).
    if a != b and not (a == "lookup" and b == "text"):
        return False
    want = norm(label)
    # `inside`: a long label inside the text, for a section where none starts with it (the
    # same choice worded "che i lavori ..." in one pratica, "i lavori ..." in another);
    # inpage.js checks an id's label the same way. Never first: "ricade" is inside "non ricade".
    return any(norm(t).startswith(want) or inside and len(want) >= 15 and want in norm(t)
               for t in (f.get("label"), f.get("context")) if t)


def offset(node, before):
    """Where a choice inserts rows and by how much later ones move: (point, dr, dp), or None.
    crawl.js pairs fields by kind and label, so two fields with the same label can pair
    wrongly; only pairs that agree on one offset count."""
    ids = {f["id"] for f in before}
    keys = {}
    for f in before:
        keys[(f["kind"], norm(f["label"]))] = keys.get((f["kind"], norm(f["label"])), 0) + 1
    by = {f["id"]: f for f in before}
    pairs = []
    for old, new in node.get("moved") or []:
        f, a, b = by.get(old), pos(old), pos(new)
        if not f or not a or not b or (new in ids and keys[(f["kind"], norm(f["label"]))] > 1):
            continue
        pairs.append((a, (b[0] - a[0], b[1] - a[1])))
    deltas = [d for _, d in pairs if d[0] > 0 and d[1] > 0]
    if not deltas:
        return None
    d = max(set(deltas), key=deltas.count)
    return min(a for a, dd in pairs if dd == d), d[0], d[1]


class Section:
    """A crawled section and the state a run leaves it in, one dictionary field at a time."""

    def __init__(self, crawled):
        self.name = crawled["section"]
        self.base = [f for f in crawled.get("fields", []) if f["kind"] != "message"]
        self.fields = [dict(f) for f in self.base]
        self.branches = crawled.get("branches", [])
        self.avail = self.branches   # choices crawled from the current state
        self.exact = True            # the ids are the crawl's own, not offsets applied to them
        self.added = False           # a "+" added rows: ids from here on are unknown
        self.applied = []            # (point, dr, dp) of each choice, base coordinates

    def find(self, kind, label, nth=1):
        order = lambda f: pos(f["id"]) or (0, 0)
        hits = sorted((f for f in self.fields if matches(f, kind, label)), key=order) or \
            sorted((f for f in self.fields if matches(f, kind, label, inside=True)), key=order)
        return hits[nth - 1] if len(hits) >= nth else None, len(hits)

    def node(self, nodes, f, option):
        for n in nodes:
            if n["kind"] == f["kind"] and n["id"] == f["id"] and n.get("option") == option:
                return n
        return None

    def moves_itself(self, f):
        """A box whose tick opens rows above it renumbers itself (the paesaggistica's art.
        136): its id before the tick isn't its id after, so it goes by label. A pair whose
        new id was on the page already is crawl.js pairing two same-label boxes, not a move."""
        base, ids = f.get("base_id", f["id"]), {x["id"] for x in self.base}
        n = self.node(self.avail, f, None) or self.node(self.branches, {**f, "id": base}, None)
        return bool(n) and any(old == base and new != old and new not in ids for old, new in n.get("moved") or [])

    def choose(self, f, option=None):
        """The state after a tick or a dropdown option; a templated option opens nothing
        the generator can know, so a dropdown whose options open fields must be literal."""
        n = self.node(self.avail, f, option)
        if n is None:
            n = self.node(self.branches, {**f, "id": f.get("base_id", f["id"])}, option)
        if n is None:
            # A choice another one opened, deeper in the crawl than the state reached.
            def every(nodes):
                for x in nodes:
                    yield x
                    yield from every(x.get("then", []))
            n = self.node(list(every(self.branches)), {**f, "id": f.get("base_id", f["id"])}, option)
        if n is None:
            return
        reveals = [dict(x) for x in n.get("reveals", []) if x["kind"] != "message"]
        hides = set(n.get("hides", []))
        off = offset(n, self.fields if n in self.avail else self.base)
        if not reveals and not hides and not off:
            return  # nothing on the page changes
        if n in self.avail and self.exact:
            # A state the crawl saw: its ids as they are.
            self.fields = [x for x in self.fields if x["id"] not in hides]
            if off:
                point, dr, dp = off
                for x in self.fields:
                    if pos(x["id"]) and pos(x["id"]) >= point:
                        x.setdefault("base_id", x["id"])
                        x["id"] = shift(x["id"], dr, dp)
            self.fields += reveals
            self.avail = n.get("then", [])
            if off:
                self.applied.append(off)
            return
        # Combined with an earlier choice the crawl tried apart: offsets, and labels from here.
        self.exact = False
        self.fields = [x for x in self.fields if x.get("base_id", x["id"]) not in hides]
        # Its reveals sit at its point (or, adding rows at the end, where they are): earlier
        # choices' offsets before there move them too.
        def moved_by_earlier(at):
            earlier = [(dr, dp) for p, dr, dp in self.applied if at and p <= at]
            return sum(d[0] for d in earlier), sum(d[1] for d in earlier)
        if off:
            point, dr, dp = off
            for x in self.fields:
                b = pos(x.get("base_id", x["id"]))
                if b and b >= point:
                    x.setdefault("base_id", x["id"])
                    x["id"] = shift(x["id"], dr, dp)
            self.applied.append(off)
        for x in reveals:
            x["base_id"] = x["id"]
            x["id"] = shift(x["id"], *moved_by_earlier(off[0] if off else pos(x["id"])))
        self.fields += reveals
        self.avail = []


def load_yaml(p):
    return yaml.safe_load(Path(p).read_text())


QUADRO = re.compile(r"^[a-z]\d?\)\s*", re.I)


def dictionary_entries(sections, name):
    """The entries that may fill a section, in order, each with the name to open it by. Quadri
    are matched without their letter: the CIL's f) is the CILA's h). The name is the entry's
    when it starts the section's, else the section's own."""
    bare, out = norm(QUADRO.sub("", name)), []
    for e in sections:
        for n in [e["section"], *e.get("aliases", [])]:
            if bare.startswith(norm(QUADRO.sub("", n))):
                out.append((e, n if norm(name).startswith(norm(n)) else name))
                break
    return out


def fill(entry, as_name, c, recipe, name):
    """One section from one dictionary entry: the steps, what doesn't fit, the asks it uses."""
    problems, asks_used = [], set()
    sec = Section(c)
    out = {"section": as_name}
    for k in ("open", "save", "go", "theirs"):
        if k in entry:
            out[k] = entry[k]
    fields, filled = [], set()
    dfields = list(entry.get("fields", []))
    # `branches`: conditions of a choice made first that renumbers every box below it (a
    # registered business or not): each box without a `when` once per branch, with its ids.
    if entry.get("branches"):
        dfields = [g for d in dfields for g in ([d] if "when" in d else
                   [{**d, "when": w} for w in entry["branches"]])]
    branch_terms = {re.match(r"\s*(\{\{[^}]*\}\})", w)[1] for w in entry.get("branches", [])}
    for i, role in enumerate(recipe.get("firms", entry.get("firms", [])) if "block" in entry else []):
        # One block per firm, the second and later added with "+", and only when the job has
        # that firm; each also under the entry's `firm_when`.
        when = " ".join(([entry["firm_when"]] if entry.get("firm_when") else [])
                        + (["{{%s:business.denominazione|empty|not}}" % role] if i else []))
        cond = {"when": when} if when else {}
        if i:
            dfields.append({"kind": "add", "anchor": entry["block"][0]["label"], "block": i + 1, **cond})
        for b in entry["block"]:
            g = json.loads(json.dumps(b).replace("{{F:", "{{" + role + ":"))
            dfields.append({**g, "anchor": entry["block"][0]["label"], "block": i + 1, **cond})
    last_select = None
    # A field under a `when` lives in its branch of the section (keyed by the condition's
    # first term): "already filed" and "this counts as filing" each open their own rows,
    # and only one is ever ticked, so neither shifts the other's ids.
    # `within`: the same, for a choice under a longer condition inside its branch (a tolerance
    # ticked only for some answers): a later field of that branch goes by label unless its own
    # condition includes that choice's.
    branches, main, changed, within = {}, sec, {}, {}
    for d in dfields:
        term = re.match(r"\s*(\{\{[^}]*\}\})", d.get("when", ""))
        sec = main if not term else branches.setdefault(term[1], copy.deepcopy(main))
        if d["kind"] == "add" or "anchor" in d:
            if d.get("block") == 1 and d["kind"] != "add":
                # The first repeat is on the page already: what it fills isn't left empty.
                start = sec.find("text", d["anchor"])[0] or {}
                after = [x for x in sec.fields if (pos(x["id"]) or (0, 0)) >= (pos(start.get("id", "")) or (0, 0))]
                hits = sorted((x for x in after if matches(x, d["kind"], d["label"])), key=lambda x: pos(x["id"]) or (0, 0))
                if len(hits) >= d.get("nth", 1):
                    filled.add((hits[d.get("nth", 1) - 1]["id"], hits[d.get("nth", 1) - 1]["label"]))
            sec.added = True
            fields.append(dict(d))
            continue
        look = sec
        if "via" in d:
            # Opened only by one option of the dropdown before it: looked up in that state.
            look = copy.deepcopy(sec)
            if last_select is None:
                problems.append(f"{name}: {d['label']!r} has `via` but no dropdown before it")
                continue
            look.choose(last_select, d["via"])
        f, count = look.find(d["kind"], d["label"], d.get("nth", 1))
        if f is None and d.get("on_page"):
            # Shown only by a combination of choices the crawl didn't try: by label, found on
            # the page (and skipped there too if it isn't, `if_present`).
            fields.append({k: v for k, v in d.items() if k not in ("note", "on_page")})
            continue
        if f is None and d.get("if_present"):
            continue  # a choice only some pratiche show
        if f is None:
            problems.append(f"{name}: no {d['kind']} labelled {d['label']!r}" +
                            (f" (only {count})" if count else "") + " in the crawl")
            continue
        filled.add((f["id"], f["label"]))
        g = {k: v for k, v in d.items() if k not in ("label", "nth", "note", "via", "if_present")}
        if d["kind"] == "textarea":
            g["kind"] = "text"
        if d["kind"] == "tick":
            g["kind"] = f["kind"]
        if f["id"].endswith(":real") and g["kind"] == "text":
            g["kind"] = "number"  # a decimal box shows 85 back as 85.00
        if f.get("max"):
            g["max"] = f["max"]  # run.py refuses a longer value before the run
        # An unconditional field that a conditional choice before it moves has no one id:
        # it goes by label, found on the page as the answers left it.
        # So does any field outside a condition whose choice changed the section: its id
        # depends on that answer.
        twins = [] if term else [b.find(d["kind"], d["label"], d.get("nth", 1))[0] for b in branches.values()]
        # The entry's `branches` exclude each other: one's renumbering isn't the other's.
        exclusive = term and term[1] in branch_terms
        moved = any(t and t["id"] != f["id"] for t in twins) or any(
            k != (term and term[1]) and not (exclusive and k in branch_terms)
            and (pos(f.get("base_id", f["id"])) or (0, 0)) >= at for k, at in changed.items()) or term and any(
            k.split()[0] == term[1] and not set(k.split()) <= set(d["when"].split())
            and (pos(f.get("base_id", f["id"])) or (0, 0)) >= at for k, at in within.items()) or (
            d["kind"] in ("radio", "checkbox", "tick") and look.moves_itself(f))
        filled.update((t["id"], t["label"]) for t in twins if t)
        if not look.added and not moved:
            g = {"id": f["id"], **g, "label": d["label"], **({"nth": d["nth"]} if d.get("nth", 1) > 1 else {})}
        else:
            g = {**g, "label": d["label"], **({"nth": d["nth"]} if d.get("nth", 1) > 1 else {})}
        fields.append(g)
        # A choice everyone makes happens in every branch too.
        for st in [sec] if term else [main, *branches.values()]:
            g2 = f if st is sec else st.find(d["kind"], d["label"], d.get("nth", 1))[0]
            if g2 is None:
                continue
            ids = [x["id"] for x in st.fields]
            if d["kind"] in ("radio", "checkbox", "tick"):
                st.choose(g2)
            elif d["kind"] == "select" and "{{" not in str(d.get("value", "")):
                st.choose(g2, d.get("value"))
            if term and [x["id"] for x in st.fields] != ids:
                # From where the section changed: the first field that moved, else the end.
                after = [pos(i) for i, j in zip(ids, [x["id"] for x in st.fields]) if i != j and pos(i)]
                at = min(after) if after else (10 ** 6, 0)
                changed[term[1]] = min(changed.get(term[1], at), at)
                within[d["when"]] = min(within.get(d["when"], at), at)
        if d["kind"] == "select":
            last_select = f
    # What the section shows that the dictionary neither fills nor leaves, in any branch.
    leave = [norm(x) for x in entry.get("leave", [])]
    shown = {(f["id"], f["label"]): f for st in [main, *branches.values()] for f in st.fields}
    # The same box, shifted in another branch's view, is the one filled.
    same = lambda g: (g["kind"], norm(g["label"]), norm(g.get("context")))
    filled_as = {same(g) for g in shown.values() if (g["id"], g["label"]) in filled}
    for f in shown.values():
        if (f["id"], f["label"]) in filled or same(f) in filled_as or f["kind"] in ("radio", "checkbox") and not entry.get("strict"):
            continue
        if any(norm(f.get("label")).startswith(x) or norm(f.get("context")).startswith(x) for x in leave):
            continue
        if f.get("value") or f.get("prefilled"):
            continue  # prefilled by the portal
        problems.append(f"{name}: {f['kind']} {f['label'][:60]!r} ({f['id']}) is in no dictionary entry")
    out["fields"] = fields
    if entry.get("note"):
        out["note"] = entry["note"]
    # The questions this section asks: those its emitted fields read (a field only some
    # pratiche show doesn't bring its question along).
    asks_used = set(re.findall(r"(?:\{\{|[+*])\s*ask\.(\w+)", json.dumps(fields)))
    return out, problems, asks_used


def generate(recipe, base=HERE):
    cat_dir = base / "catalogue" / recipe["comune"]
    catalogue = json.loads((cat_dir / f"{recipe['sportello']}-wizard.json").read_text())
    crawl = json.loads((cat_dir / "sections" / f"{recipe['crawl']}.json").read_text())
    drift = cat_dir / "drift.json"
    changed = json.loads(drift.read_text())["pratiche"].get(recipe["crawl"], {}).get("differences") if drift.exists() else None
    if changed:
        raise GenError(f"the portal changed {recipe['crawl']} since its crawl ({'; '.join(changed)}): crawl it again")
    dictionary = load_yaml(base / "dictionary" / f"{recipe.get('dictionary', 'fnsuap')}.yaml")

    if "operations" not in catalogue:
        # A catalogue of the wizard's tree only (the SUAP's so far): the pratica's sections are
        # the crawled draft's, in its order, and the recipe gives the wizard.
        op = {"label": recipe["operation"], "scelte": []}
        # A batch crawl (crawl.js batch) says which sections the base pratica has and which a
        # scelta adds; the recipe's scelte pick those.
        names = [s["section"] for s in crawl["sections"] if not s.get("scelta") or s["scelta"] in recipe.get("scelte", [])]
        # A scelta the batch tried only once (its first pratica): its sections from there.
        for sc in recipe.get("scelte", []):
            if any(s.get("scelta") == sc for s in crawl["sections"]):
                continue
            for other in sorted((cat_dir / "sections").glob("*.json")):
                found = [s for s in json.loads(other.read_text())["sections"] if s.get("scelta") == sc]
                if found:
                    crawl["sections"] += [{"section": s["section"], "same_as": f"{other.stem}#{s['section']}", "scelta": sc} for s in found]
                    names += [s["section"] for s in found]
                    break
            else:
                raise GenError(f"no crawl has tried the scelta {sc!r}")
        if "wizard" not in recipe:
            raise GenError("the catalogue has no pratica for this operation: the recipe needs `wizard`")
    else:
        # Several operations in one pratica (a CILA with its landscape authorization): each one's
        # sections with its scelte, and the pratica is their union.
        labels = recipe["operation"] if isinstance(recipe["operation"], list) else [recipe["operation"]]
        idx = []
        for label in labels:
            o = next((o for o in catalogue["operations"] if o["label"] == label), None)
            if not o:
                raise GenError(f"no operation {label!r} in the catalogue")
            # Each scelta drops and adds by index, so a second one keeps what the first added.
            own = list(o["sections"])
            for s in recipe.get("scelte", []):
                sc = next((x for x in o.get("scelte", []) if x["option"] == s), None) or \
                    (_ for _ in ()).throw(GenError(f"no scelta {s!r} for {label}"))
                own = [i for i in own if i not in sc.get("drops", [])] + [i for i in sc.get("adds", []) if i not in own]
            idx += [i for i in own if i not in idx]
        op = {"label": labels[0] if len(labels) == 1 else labels}
        names = [catalogue["sections"][i] for i in idx]
        if len(labels) > 1:
            # The portal's own order is the combination's crawl's, which must hold just these.
            have = [s["section"] for s in crawl["sections"]]
            missing = [n for n in names if not any(norm(h) == norm(n) for h in have)]
            # One section both operations have under two names (the semplificata's "ubicazione
            # dell'intervento" is the CILA's "Localizzazione"): shown once, the same fields.
            fps = {s.get("fingerprint") for s in crawl["sections"]} - {None}
            seen = {norm(s["section"]) for f in (cat_dir / "sections").glob("*.json")
                    for s in json.loads(f.read_text()).get("sections", []) if s.get("fingerprint") in fps}
            missing = [n for n in missing if norm(n) not in seen]
            extra = [h for h in have if not any(norm(h) == norm(n) for n in names)]
            if missing or extra:
                raise GenError(f"{recipe['crawl']} isn't a crawl of these operations together: "
                               f"missing {missing}, more {extra}")
            # Each crawled section once, under the name the combination shows ("Dati catastali" and
            # "dati catastali" are one).
            names = [h for h in have if any(norm(h) == norm(n) for n in names)]
    # A batch crawl names a section it had seen elsewhere (`same_as`: "pratica#section").
    import catalogue as cat_py
    others = {}
    for i, s in enumerate(crawl["sections"]):
        if s.get("same_as"):
            try:
                seen = cat_py.resolve(s["same_as"], others, cat_dir / "sections")
            except (StopIteration, FileNotFoundError, ValueError):
                raise GenError(f"{s['section']}: {s['same_as']} isn't in the catalogue")
            crawl["sections"][i] = {**seen, "section": s["section"]}
    crawled = {norm(s["section"]): s for s in crawl["sections"]}

    problems, out_sections, asks_used = [], [], set()
    first = [next((n for n in names if norm(n).startswith(norm(x))), x) for x in recipe.get("first", [])]
    for name in first + [n for n in names if n not in first]:
        if name not in names:
            problems.append(f"{name}: in `first` but not in this pratica")
            continue
        if DECLARATIONS.search(name):
            continue
        # The recipe's own entries first: a case only this pratica has.
        # An entry for one subject (`subject: impresa`, the business itself declaring) serves only
        # recipes for that subject; one without serves all.
        candidates = [(e, n) for e, n in dictionary_entries(recipe.get("sections", []) + dictionary["sections"], name)
                      if e.get("subject", recipe.get("subject")) == recipe.get("subject")]
        candidates.sort(key=lambda en: "subject" not in en[0])  # the subject's own first
        if not candidates:
            problems.append(f"{name}: no dictionary entry")
            continue
        if candidates[0][0].get("omit"):
            continue
        c = crawled.get(norm(name)) or next((v for k, v in crawled.items() if k.startswith(norm(name)[:30])), None)
        if not c or c.get("error"):
            problems.append(f"{name}: not crawled" + (f" ({c['error']})" if c else ""))
            continue
        # The first entry that fits: the CILA's "Calcolo del contributo" is one radio, the
        # SCIA's adds the rule that makes it free.
        tried = []
        for entry, as_name in candidates:
            out, probs, used = fill(entry, as_name, c, recipe, name)
            if not probs:
                break
            tried.append(probs)
        else:
            problems.extend(min(tried, key=len))  # the entry that came closest
            continue
        asks_used |= used
        out_sections.append(out)

    for name in recipe.get("after", []):
        entry = next((e for e in dictionary.get("after", []) if e["section"] == name), None)
        if not entry:
            problems.append(f"{name}: no dictionary entry for the screen")
            continue
        slots = crawl.get("slots", {}).get(name)
        if slots is None and entry.get("fields"):
            problems.append(f"{name}: this pratica's slots aren't crawled yet (crawl.js slots() on the screen)")
            continue
        out = {k: v for k, v in entry.items() if k not in ("fields", "note")}
        fields = []
        for d in entry.get("fields", []) + recipe.get("attachments", {}).get(name, []):
            slot = next((s for s in slots if norm(s["title"]).startswith(norm(d["label"]))), None)
            if not slot:
                if d.get("on_page"):
                    # A slot only some answers add (an onerosa CILA's contributo receipt): the
                    # crawl's screen hasn't it, the run finds it by label under its `when`.
                    fields.append({k: v for k, v in d.items() if k != "on_page"})
                    for t in re.findall(r"(?:\{\{|[+*])\s*ask\.(\w+)", json.dumps(d)):
                        asks_used.add(t)
                continue  # this pratica has no such slot
            if slot.get("accepts") and all(t.endswith(".p7m") for t in slot["accepts"].split(";")):
                # Signed files only: the person's to sign and attach, never a plain copy.
                out["theirs"] = out.get("theirs", []) + [d["label"]]
                continue
            # What the slot takes, so pre-flight can refuse a file it wouldn't.
            fields.append(dict(d) | ({"accepts": slot["accepts"]} if slot.get("accepts") else {}))
            for t in re.findall(r"(?:\{\{|[+*])\s*ask\.(\w+)", json.dumps(d)):
                asks_used.add(t)
        if slots is not None:
            # Every slot that takes only signed files is the person's, named or not.
            for s in slots:
                if s.get("accepts") and all(t.endswith(".p7m") for t in s["accepts"].split(";")):
                    title = s["title"][:60]
                    if not any(norm(title).startswith(norm(t)) or norm(t).startswith(norm(title)) for t in out.get("theirs", [])):
                        out["theirs"] = out.get("theirs", []) + [title]
            for s in slots:
                hit = any(norm(s["title"]).startswith(norm(d["label"])) for d in fields)
                # The person's: the entry's own, and the signed-only slots handed to them above.
                theirs = any(norm(s["title"]).startswith(norm(t)) for t in entry.get("theirs", []) + out.get("theirs", []))
                if s.get("required") and not hit and not theirs:
                    problems.append(f"{name}: required slot {s['title'][:70]!r} is in no dictionary entry")
        out["fields"] = fields
        if entry.get("note"):
            out["note"] = entry["note"]
        out_sections.append(out)

    if problems:
        raise GenError("\n".join(problems))
    for t in recipe.get("asks_used", []):
        asks_used.add(t)
    for t in re.findall(r"(?:\{\{|[+*])\s*ask\.(\w+)", json.dumps([recipe.get("yours"), recipe.get("supports"), recipe.get("rules")])):
        asks_used.add(t)
    # The dictionary's questions, the recipe's wording over them; a question only this pratica
    # asks (one `supports` or `rules` reads) is the recipe's alone.
    asks = {}
    for k, v in list(dictionary["asks"].items()) + [(k, {}) for k in recipe.get("asks", {}) if k not in dictionary["asks"]]:
        if k in asks_used:
            asks[k] = {**v, **recipe.get("asks", {}).get(k, {})}
    missing = (asks_used - set(asks)) | {k for k, a in asks.items() if "type" not in a}
    if missing:
        raise GenError(f"asks with no definition in the dictionary: {sorted(missing)}")
    # Yes/no questions asked as one checklist (run.py preflight's `groups`).
    groups = {g: v for g, v in {**dictionary.get("groups", {}), **recipe.get("groups", {})}.items()
              if any(a.get("group") == g for a in asks.values())}

    ruled = rules_block(recipe["rules"], asks, groups) if recipe.get("rules") else None

    wizard = recipe.get("wizard")
    if wizard and crawl.get("screens") is not None:
        # The screens a batch crawl saw after the operations: a recipe's step for a screen
        # this pratica doesn't show (a closure has no destinatario) is dropped.
        seen = {norm(x["screen"]) for x in crawl["screens"]}
        after = False
        kept = []
        for w in wizard:
            if after and norm(w["screen"]) not in seen:
                continue
            after = after or norm(w["screen"]).startswith("scelta operazioni")
            kept.append(w)
        wizard = kept
    wizard = wizard or [
        {"screen": "Assegna una descrizione alla pratica",
         "fill": {"css": 'input[placeholder^="Inserire la descrizione"]', "value": "{{ask.descrizione}}", "max": 60}, "confirm": "CONFERMA"},
        {"screen": "Controlla l'accesso alla pratica", "confirm": "CONFERMA"},
        {"screen": "Scelta settore attività", "tree": catalogue["sector"], "confirm": "CONFERMA"},
        {"screen": "Scelta operazioni", "tree": op["label"], "confirm": "CONFERMA"},
        {"screen": "Scelta del destinatario della pratica", "confirm": "CONFERMA"},
    ]
    if recipe.get("scelte") is not None and "wizard" not in recipe:
        wizard.append({"screen": "Scelte eventuali", **({"tree": recipe["scelte"][0] if len(recipe["scelte"]) == 1 else recipe["scelte"]} if recipe["scelte"] else {}),
                       **({"answers": recipe["si_no"]} if recipe.get("si_no") else {}),
                       "confirm": "CONFERMA"})
    return {
        "procedure": recipe["procedure"], "title": recipe["title"],
        "generated": f"gen.py from {recipe['crawl']} ({crawl.get('crawled', '?')[:10]}) and dictionary/{recipe.get('dictionary', 'fnsuap')}.yaml",
        "yours": recipe["yours"], **({"groups": groups} if groups else {}), "asks": asks, "supports": recipe.get("supports", []),
        **({"rules": ruled} if ruled else {}),
        "start": recipe.get("start", catalogue["start"]), "logged_in": recipe.get("logged_in", "https://suap.impresainungiorno.gov.it/fnsuap/"),
        "wizard": wizard, "sections": out_sections,
    }


def dump(steps):
    """YAML in the hand-written files' shape: one field per line."""
    class Flow(dict):
        pass
    yaml.add_representer(Flow, lambda d, v: d.represent_mapping("tag:yaml.org,2002:map", v.items(), flow_style=True))
    yaml.add_representer(dict, lambda d, v: d.represent_mapping("tag:yaml.org,2002:map", v.items()))
    s = dict(steps)
    s["sections"] = [{**sec, "fields": [Flow(f) for f in sec["fields"]]} for sec in steps["sections"]]
    s["asks"] = {k: Flow(v) for k, v in steps["asks"].items()}
    head = ("# Generated by claude/skills/pratica/portal/gen.py: don't edit, change the dictionary or the\n"
            "# recipe and generate again.\n")
    return head + yaml.dump(s, allow_unicode=True, sort_keys=False, width=200)


def check(gen, hand):
    """The generated sections against a hand-written file: the same fields, by id or label, in
    the same order, with the same values. A generated field with a `when` that the hand file
    hasn't is a case it didn't walk: listed apart, not a difference."""
    diffs, extra = [], []
    key = lambda f: f.get("id") or (f"add {f['anchor']}" if f["kind"] == "add" else
                                     f"label {f.get('label')!r} {f.get('anchor', '')}{f.get('block', '')}/{f.get('nth', 1)}")
    hs = {norm(s["section"]): s for s in hand["sections"]}
    for s in gen["sections"]:
        k = next((k for k in hs if k.startswith(norm(s["section"])) or norm(s["section"]).startswith(k)), None)
        h = hs.pop(k, None)
        if not h:
            diffs.append(f"{s['section']}: not in the hand-written file")
            continue
        hand_keys = [key(b) for b in h["fields"]]
        gen_fields = [a for a in s["fields"] if key(a) in hand_keys or "when" not in a]
        extra += [f"{s['section']}: {key(a)} when {a['when']}" for a in s["fields"] if a not in gen_fields]
        for i, (a, b) in enumerate(zip(gen_fields, h["fields"])):
            if key(a) != key(b) or a.get("value") != b.get("value"):
                diffs.append(f"{s['section']} #{i + 1}: generated {key(a)} = {a.get('value')!r}, hand {key(b)} = {b.get('value')!r}")
            elif a.get("when") != b.get("when"):
                extra.append(f"{s['section']}: {key(a)} when {a.get('when')} (hand: {b.get('when')})")
        if len(gen_fields) != len(h["fields"]):
            diffs.append(f"{s['section']}: {len(gen_fields)} fields generated, {len(h['fields'])} by hand")
    diffs += [f"{h['section']}: only in the hand-written file" for h in hs.values()]
    return diffs, extra


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("recipe", type=Path)
    ap.add_argument("--out", type=Path)
    ap.add_argument("--check", type=Path, help="a hand-written steps file to compare with")
    a = ap.parse_args()
    try:
        steps = generate(load_yaml(a.recipe))
    except GenError as e:
        sys.exit(f"gen.py: not generated:\n{e}")
    if a.out:
        a.out.write_text(dump(steps))
    if a.check:
        diffs, extra = check(steps, load_yaml(a.check))
        print("\n".join(diffs) or "same as the hand-written file")
        if extra:
            print("conditions the hand-written file doesn't have (cases it didn't walk):\n  " + "\n  ".join(extra))
        sys.exit(1 if diffs else 0)
    if not a.out:
        print(dump(steps))


if __name__ == "__main__":
    main()
