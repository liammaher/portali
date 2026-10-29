#!/usr/bin/env python3
"""Fill a portal pratica from the vault in a real browser, then hand it to the person.

  run.py preflight --steps STEPS.yaml [--answers A.json] VAULT
      Before anything opens: prints JSON with why this case isn't supported (the steps
      file's `supports:`, and its `rules:` table, which says whether the case is this pratica
      at all), the answers it proposes for the person to confirm (an ask's
      `propose:`), the questions still to ask (its `asks:`, branch questions first), the
      vault fields missing or expired, the files to attach (each within the portal's 9.77 MB
      and of a type its slot takes) and whether they fit the pane, and the person's own steps. Exits 0 when `ready`, 3 when not.

  run.py plan --steps STEPS.yaml --answers A.json
              (--values - | --server S.json --token NAME | --vault-dir DIR [--keychain SERVICE])
      Resolves every value the steps need and prints what goes where. No browser.

  run.py page --steps STEPS.yaml --answers A.json VAULT --out DIR [--plain]
      Writes the scripts for the Claude browser pane, each run with one call on the
      portal's page: check.js says which of runner.js (inpage.js) and steps.js (the steps,
      no personal data) the pane's storage doesn't already hold byte for byte; run.js starts
      a run with the person's values; install.js is runner.js and steps.js together.
      Claude writes every byte of a call, so run.js would put the values through the
      conversation. It doesn't: run.js carries them sealed under a code that a window on
      the person's screen shows (not printed, not in a file) and that they type into a box
      the runner opens on the portal's page. --plain carries them in the clear instead (made-up
      data, or a person who accepts that), and is the only choice where no window can open.
      bench/rerun.py does it for many rehearsals at once. inpage.js says more.

  run.py run  --steps STEPS.yaml --answers A.json VAULT --out DIR
              [--cdp URL | --profile DIR] [--close]
      Opens the portal in a Playwright window (its own Chromium) and waits for the person
      to log in, then runs inpage.js there, as in the pane: the wizard, each section filled,
      saved and read back, then the draft loaded again and every value read once more. It
      stops before what is the person's: the declarations, the attachments the steps file
      doesn't name, the payment, signing and sending, and shows the draft, left open for
      them unless --close. Writes DIR/report.json (timings, what each section kept, and when
      it stops a `snapshot` of the page with no value in it): no value is in it, so Claude
      may read it. DIR/private/ (kept.json, held.json, shown.json) and DIR/review.html (the page
      the person reviews: what's left for them, the steps file's `yours`, every value, and
      what a failed read-back wanted and found) are the person's, for Claude to send and
      never to read. There is no screenshot: a failed page holds the person's data.

A run that stops says where (report.json `at`: the wizard screen, or the section and
field). `page` or `run` with --resume REPORT picks it up on the same draft, from that
section, with --fix FIX for what the page really has there; DIR/drift.json records the
fixes, and the fresh load at the end still checks every section.

STEPS.yaml is a portal's steps file (rehearsals/*/portal-steps.yaml). Values come from
the vault, VAULT as for plan. --values - takes what `pvault get` printed for the steps'
categories (`pvault get 'business.*' 'locale.*' '*:identity.*' ...`, piped in: one Touch ID
approval, nothing on disk). --server is the pilot vault's JSON ("url" and "tokens") and a
token's name in it; --vault-dir is a real vault, read over its socket with the token in
$VAULT_TOKEN or the Keychain (--keychain, default fill.py's), as fill.py reads it. The
token reads the categories the steps name. Asks come from A.json. --cdp attaches
to a Chrome the person started with --remote-debugging-port, for a site that refuses a
browser Playwright launched; otherwise Chromium starts with its own profile (--profile,
default ~/.cache/pratica-portal/profile), never the person's own, so a login there
lasts as long as the portal keeps it.

A `kind: file` field attaches a file: its value is a path (usually an {{ask.x}}), and
its bytes travel in run.js. With `page` they pass through the conversation, so all of a
run's files together may be at most 256 KB (PAGE_FILES_LIMIT); `run` evaluates run.js
itself and takes any size. `page --file-urls MAP.json` ({path: https URL}) sends a file
by link instead: run.js carries its URL, size and sha256, and the runner fetches it in the
portal's page and stops if the link serves other bytes; such files don't count to 256 KB.

No model is called during a run. A step that doesn't match what the steps file expects
(a label, a lookup with no single answer, a value the portal didn't keep) stops the run:
that is the point where Claude looks at the page and fixes the steps file.
"""

import argparse
import base64
import hashlib
import hmac
import json
import mimetypes
import os
import re
import secrets
import shutil
import subprocess
import sys
import time
from datetime import date
from pathlib import Path

import yaml

sys.path[:0] = [str(Path(__file__).resolve().parent.parent / "fill"), str(Path(__file__).resolve().parent.parent),
                str(Path(__file__).resolve().parent)]
sys.dont_write_bytecode = True  # no __pycache__ in the skill's folder
import fill  # noqa: E402  (its vault client: the pilot over TCP, a real vault over its socket)
import rules  # noqa: E402  (the library's rules tables: which pratica a case needs)
import review  # noqa: E402  (the page the person reviews)

# Never clicked, whatever a steps file says: these are the person's taps.
FORBIDDEN = re.compile(r"pago\s*pa|\binvia\b|\bpaga\b|procedi con il pagamento|firma", re.I)
# Never opened: the dichiarante's own statements.
DECLARATIONS = re.compile(
    r"consapevolezza|informativa sulla privacy|requisiti di o|altre dichiarazioni|"
    r"dichiarazioni ai sensi dell'art\. 6|rispetto sulle tasse|art\. ?75", re.I)

DUG = {"VIA", "VIALE", "PIAZZA", "CORSO", "LARGO"}
# run.js for the pane is one tool call's text: a receipt or a small scan, not a planimetria.
# Larger files need another route into the page (docs/north-star.md, open question 7).
PAGE_FILES_LIMIT = 256 * 1024
# What impresainungiorno takes in one slot: "Max: 9,77 MB" beside each, 10,000 KB.
SLOT_MAX_BYTES = 10_000 * 1024
FILE_TYPES = {".p7m": "application/pkcs7-mime", ".pdf": "application/pdf"}
CARICA = {"titolare": "TITOLARE", "legale_rappresentante": "LEGALE RAPPRESENTANTE"}
FORMA = {
    "ditta_individuale": "IMPRESA INDIVIDUALE",
    "srl": "SOCIETA' A RESPONSABILITA' LIMITATA",
    "srls": "SOCIETA' A RESPONSABILITA' LIMITATA SEMPLIFICATA",
    "snc": "SOCIETA' IN NOME COLLETTIVO",
    "sas": "SOCIETA' IN ACCOMANDITA SEMPLICE",
    "spa": "SOCIETA' PER AZIONI",
}


def _carica(v):
    for role in [r.strip() for r in v.split(",")]:
        if role in CARICA:
            return CARICA[role]
    raise ValueError(f"no portal carica for ruolo {v!r}")


def _split_street(v):
    words = v.split()
    if len(words) > 1 and words[0].upper() in DUG:
        return words[0].upper(), " ".join(words[1:])
    return "ALTRO", v


# The province names the portal's free-text boxes want ("Iscritto all'ordine di ..."): the
# ones a run has needed so far; another stops the run (a KeyError) instead of guessing.
PROVINCE = {"NA": "NAPOLI", "CE": "CASERTA", "SA": "SALERNO", "AV": "AVELLINO", "BN": "BENEVENTO",
            "RM": "ROMA", "MI": "MILANO", "TO": "TORINO"}
# The Scheda anagrafica's "In qualità di" for the title the person holds (a tenant is
# "Altro" + "Locatario").
QUALITA = {"proprietario": "Proprietario", "comproprietario": "Comproprietario", "usufruttuario": "Usufruttuario",
           "amministratore di condominio": "Amministratore di condominio"}


def _albo_tipo(v):
    """An ordine's name ("Ordine degli Architetti P.P.C.") as the SUE's list has it."""
    v = v.lower()
    for word, option in (("architett", "Architetto"), ("ingegner", "Ingegnere"), ("geometr", "Geometra"), ("perit", "Perito")):
        if word in v:
            return option
    return "Altro"


def _qualita(v):
    return QUALITA.get(v.strip().lower(), "Altro")


def _titolo_pratica(v):
    """A SUE quadro a)'s dropdown option for the title: its own name, else "altro" (a
    tenant, a comodatario: then + specificare)."""
    return v.strip().lower() if v.strip().lower() in QUALITA else "altro"


# The SUE's destinazione d'uso dropdown: these three, anything else "altro" + its name.
DESTINAZIONI = {"residenziale", "industriale", "commerciale"}


def _destinazione(v):
    return v.strip().lower() if v.strip().lower() in DESTINAZIONI else "altro"


class Filters(dict):
    """`is_<answer>` is true when the value is that answer, spaces as underscores
    ({{ask.titolo_tipo|is_permesso_di_costruire}}): a choice's branch in a `when`. `atleast_<n>`
    is true when the number is n or more (the 20 m² the historic centre's Piano requires)."""
    def __missing__(self, name):
        if name.startswith("atleast_") and name[8:].isdigit():
            return lambda v: "true" if float(str(v or 0).replace(",", ".")) >= int(name[8:]) else "false"
        if not name.startswith("is_"):
            raise KeyError(name)
        return lambda v: "true" if re.sub(r"[^a-z0-9]+", "_", str(v).strip().lower()).strip("_") == name[3:] else "false"


FILTERS = Filters({
    "upper": str.upper,
    "albo_tipo": _albo_tipo,
    "sigla": lambda v: ".".join(v.strip().upper()) + "." if v.strip().isalpha() and len(v.strip()) <= 5 else v,
    "sesso": lambda v: {"F": "Femmina", "M": "Maschio"}[v.strip().upper()],
    "provincia_nome": lambda v: PROVINCE[v.strip().upper()],
    "qualita": _qualita,
    "qualita_is_altro": lambda v: "true" if _qualita(v) == "Altro" else "false",
    "qualita_altro": lambda v: v.strip().capitalize(),
    "titolo_pratica": _titolo_pratica,
    "titolo_is_altro": lambda v: "true" if _titolo_pratica(v) == "altro" else "false",
    # A title usually held alone (a.1): the proposal pre-flight asks to confirm.
    "titolo_solo": lambda v: "true" if v.strip().lower() in ("proprietario", "usufruttuario", "amministratore di condominio") else "false",
    "destinazione": _destinazione,
    "destinazione_is_altro": lambda v: "true" if _destinazione(v) == "altro" else "false",
    "filled": lambda v: "true" if v else "false",
    "rea_num": lambda v: v.split("-")[-1].strip(),
    "date_it": lambda v: "/".join(reversed(v.split("-"))),
    "phone_it": lambda v: re.sub(r"^\+39|\s", "", v),
    "dug": lambda v: _split_street(v)[0],
    "street": lambda v: _split_street(v)[1],
    "carica": _carica,
    "forma": lambda v: FORMA[v],
    "categoria": lambda v: re.sub(r"^([A-F])/?(\d)$", r"\g<1>0\2", v.replace(" ", "").upper()),
    "empty": lambda v: "true" if not v else "false",
    "not": lambda v: "false" if truthy(v) else "true",
    "positive": lambda v: "true" if float(str(v or 0).replace(",", ".")) > 0 else "false",
})


def truthy(v):
    """True for a yes; a `when` of several words ("{{ask.a|not}} {{ask.b}}") wants every one."""
    words = str(v).strip().lower().split()
    return bool(words) and all(w in ("true", "1", "yes", "si", "sì") for w in words)


class StepError(Exception):
    pass


# --- values -----------------------------------------------------------------------

class Values:
    """The vault fields and answers a steps file refers to."""

    def __init__(self, answers, vault=None, fields=None, asks=None):
        """From the vault with a token, or from fields already read (pvault get's JSON).
        asks is the steps file's `asks:`, for the defaults of what answers leave out."""
        self.vault, self.fields = vault, fields
        self.answers, self.asks = answers, asks or {}
        self.cache = {}
        self._titolare_slug = None

    @property
    def titolare(self):
        # Found when a template first names t:, so pre-flight can report a missing one.
        if self._titolare_slug is None:
            self._titolare_slug = self._titolare()
        return self._titolare_slug

    def _get(self, path):
        status, body = self.vault.call("GET", path)
        if status == 404:
            return []
        if status != 200:
            raise StepError(f"reading the vault: {status} {body}")
        return body

    def category(self, cat):
        if cat not in self.cache:
            if self.fields is not None:
                self.cache[cat] = {k: v for k, v in self.fields.items() if k.startswith(cat + ".") and v is not None}
            else:
                self.cache[cat] = {r["id"]: r["value"] for r in self._get("/vault/fields/category/" + cat)}
        return self.cache[cat]

    def _titolare(self):
        ids = list(self.fields) if self.fields is not None else [f["id"] for f in self._get("/vault/fields")]
        slugs = sorted({i.split(":")[0] for i in ids if i.endswith(":ruolo.tipo")})
        found = [s for s in slugs if "titolare" in self.category(s + ":ruolo").get(s + ":ruolo.tipo", "")
                 or "legale_rappresentante" in self.category(s + ":ruolo").get(s + ":ruolo.tipo", "")]
        if len(found) != 1:
            raise StepError(f"want one titolare or legale rappresentante, found {found or 'none'}")
        return found[0]

    def raw(self, key):
        """An ask's answer as given; a checklist's answer (the items ticked, as a list
        under the group's key) answers each of its items, ticked or not."""
        v = self.answers.get(key)
        group = self.asks.get(key, {}).get("group")
        if v is None and group and isinstance(self.answers.get(group), list):
            v = key in self.answers[group]
        return v

    def field(self, ref):
        if ref.startswith("ask."):
            v = self.raw(ref[4:])
            if v is None and "default" in self.asks.get(ref[4:], {}):
                v = self.render(self.asks[ref[4:]]["default"])
            return None if v is None else str(v).lower() if isinstance(v, bool) else str(v)
        if ref.startswith("t:"):
            ref = self.titolare + ":" + ref[2:]
        # A role of this pratica (answers.json "people": {"c": "luca", ...}) names a person.
        role, sep, rest = ref.partition(":")
        if sep and role in (self.answers.get("people") or {}):
            ref = self.answers["people"][role] + ":" + rest
        cat = ref.rsplit(".", 1)[0]
        return self.category(cat).get(ref)

    def sum(self, ref):
        """`ask.a+2*ask.b`: a box the portal wants as a total of what was asked (rooms, beds)."""
        total = 0
        for term in ref.split("+"):
            n, _, r = term.strip().rpartition("*")
            v = self.field(r.strip())
            if v is None:
                return None
            total += int(n or 1) * float(str(v or 0).replace(",", "."))
        return f"{total:g}"

    def render(self, template):
        """A template's value, or None when a field it names is missing."""
        def one(m):
            ref, *filters = [p.strip() for p in m.group(1).split("|")]
            v = self.sum(ref) if "+" in ref or "*" in ref else self.field(ref)
            if v is None:
                if "empty" in filters:
                    v = ""
                else:
                    raise KeyError(ref)
            for f in filters:
                v = FILTERS[f](v)
            return v
        try:
            return re.sub(r"\{\{(.+?)\}\}", one, str(template))
        except KeyError:
            return None


def plan(steps, values):
    """Each field with its value; raises on a missing required value."""
    out, missing = [], []
    for w in steps.get("wizard", []):
        v = values.render(w["fill"]["value"]) if "fill" in w else None
        if v and w["fill"].get("max") and len(v) > w["fill"]["max"]:
            missing.append(f"{w['screen']}: {v!r} is longer than the portal takes ({w['fill']['max']})")
    for sec in steps["sections"]:
        for f in sec["fields"]:
            if "when" in f and not truthy(values.render(f["when"]) or ""):
                continue
            v = values.render(f["value"]) if "value" in f else None
            name = f.get("label") or f.get("id") or f.get("css")
            if "value" in f and v is None and not f.get("optional"):
                missing.append(f"{sec['section']}: {name} ({f['value']})")
            if f["kind"] == "file" and v and not Path(v).expanduser().is_file():
                missing.append(f"{sec['section']}: {name}: no file {v}")
            if f.get("max") and v and len(v) > f["max"]:
                missing.append(f"{sec['section']}: {name}: {v!r} is longer than the portal takes ({f['max']})")
            out.append({"section": sec["section"], "id": f.get("id") or f.get("css"), "kind": f["kind"],
                        "label": f.get("label"), "value": v})
    if missing:
        raise StepError("missing values:\n  " + "\n  ".join(missing))
    return out


# --- pre-flight and branch coverage --------------------------------------------------

ASK_TYPES = {"text", "number", "yes_no", "date", "choice", "file"}


def refs_in(template):
    """The fields a template reads, each with its filters (each term of a sum on its own)."""
    return [(t.strip().rpartition("*")[2].strip(), parts[1:]) for parts in
            ([p.strip() for p in m.split("|")] for m in re.findall(r"\{\{(.+?)\}\}", str(template)))
            for t in parts[0].split("+")]


def templates(steps):
    """Every template in a steps file, with where it is."""
    for w in steps.get("wizard", []):
        if "fill" in w:
            yield f"wizard: {w['screen']}", w["fill"]["value"], {}
    for sec in steps["sections"]:
        for f in sec["fields"]:
            for k in ("when", "value"):
                if k in f:
                    yield f"{sec['section']}: {f.get('label') or f.get('id') or f.get('css')}", f[k], f
    for y in steps.get("yours", []):
        yield "yours", y, {}
    for c in steps.get("supports", []):
        for k in ("when", "value", "else"):
            if k in c:
                yield "supports", c[k], {}
    for v in steps.get("rules", {}).get("facts", {}).values():
        yield "rules", v, {}


def check_asks(steps):
    """The asks block itself, and the order to ask in. Every ask a field reads has its
    question, and every question is read by a field, a wizard screen or `supports`: one
    only the person's own steps mention is not worth asking. A yes/no in a `group` is an
    item of that group's checklist, asked as one question."""
    asks, groups = steps.get("asks", {}), steps.get("groups", {})
    reads = lambda wheres: {r[4:] for where, t, _ in templates(steps) if wheres(where)
                            for r, _ in refs_in(t) if r.startswith("ask.")}
    used = reads(lambda w: w != "yours")
    lacking = sorted((used | reads(lambda w: w == "yours")) - set(asks))
    if lacking:
        raise StepError(f"steps file: no question in asks: for {', '.join(lacking)}")
    unused = sorted(set(asks) - used)
    if unused:
        raise StepError(f"steps file: no field reads asks.{', asks.'.join(unused)}: drop it")
    for k, a in asks.items():
        if "group" in a:
            if a["group"] not in groups or a.get("type") != "yes_no" or not a.get("item") or "propose" in a:
                raise StepError(f"steps file: asks.{k} is in a group, so it needs type yes_no, an item, "
                                "its group in groups:, and no propose (the checklist's \"none\" is the proposal)")
        elif a.get("type") not in ASK_TYPES or not a.get("question") or (a["type"] == "choice" and not a.get("options")):
            raise StepError(f"steps file: asks.{k} needs a question and a type ({', '.join(sorted(ASK_TYPES))}), "
                            "and options for a choice")
    for g, d in groups.items():
        if not d.get("question") or not any(a.get("group") == g for a in asks.values()):
            raise StepError(f"steps file: groups.{g} needs a question and at least one ask in it")
    # The asks that decide the branch come first: their answers can end the pratica here.
    branch = {r[4:] for c in steps.get("supports", []) for k in ("when", "value")
              for r, _ in refs_in(c.get(k, "")) if r.startswith("ask.")}
    branch |= {r[4:] for where, t, _ in templates(steps) if where == "rules" for r, _ in refs_in(t) if r.startswith("ask.")}
    return sorted(asks, key=lambda k: k not in branch)


def needed(steps, values):
    """The asks this case needs answered, each with the unanswered asks its need hangs on
    (empty: needed whatever they say). A field whose `when` is false for this case needs
    nothing, so its questions aren't asked."""
    need = {}

    def add(keys, deps=()):
        for k in keys:
            need[k] = set() if (k in need and not need[k]) or not deps else need.get(k, set()) | set(deps)

    asked = lambda tmpl: [r[4:] for r, _ in refs_in(tmpl) if r.startswith("ask.")]

    def gate(when):
        """A `when`'s words in order, each a gate for the next ("onerosa, and then paid?"):
        a later word's asks hang on the earlier ones; a false word ends it. False when this
        case's answers already make it false."""
        deps = []
        for word in re.findall(r"\{\{.+?\}\}", when):
            add(asked(word), deps)
            v = values.render(word)
            if v is None:
                deps = deps + [k for k in asked(word) if values.field("ask." + k) is None]
            elif not truthy(v):
                return False
        return True

    for c in steps.get("supports", []):
        add(asked(c.get("when", c.get("value", ""))))
    for fact in ruled(steps, values)[1]:
        add(asked(steps["rules"]["facts"][fact]))
    for w in steps.get("wizard", []):
        if "fill" in w:
            add(asked(w["fill"]["value"]))
    for sec in steps["sections"]:
        for f in sec["fields"]:
            deps = ()
            if "when" in f:
                if not gate(f["when"]):
                    continue
                w = values.render(f["when"])
                if w is None:
                    deps = [k for k in asked(f["when"]) if values.field("ask." + k) is None]
            add(asked(f.get("value", "")), deps)
    return need


def ruled(steps, values):
    """What the rules table (steps `rules:`) says of this case: why it isn't this pratica,
    and the facts it still waits on. Each fact is the recipe's value or an ask's answer.
    The table's main pratica must be this steps file's procedure, at its wizard's sector
    and operation; every other pratica it adds must be one this pratica files (`handles`),
    and every one this pratica files one the case needs."""
    r = steps.get("rules")
    if not r:
        return [], []
    t = rules.table(r["table"])
    facts = {}
    for name, fact in t["facts"].items():
        v = r["facts"][name]
        v = values.render(v) if isinstance(v, str) and "{{" in v else v
        if v is not None and rules.fact_value(fact, v) is not None:
            facts[name] = rules.fact_value(fact, v)
    d = rules.decide(t, facts)
    if d["conflict"]:
        raise StepError(f"rules table {r['table']}: rules {', '.join(d['conflict'])} disagree (validate the library)")
    out = [x["reason"] for x in d["refused"]]
    if d["ask"]:
        return out, d["ask"]
    m = d["main"]
    where = lambda o: " at " + " › ".join(o["portal"][k] for k in ("settore", "operazione") if k in o["portal"]) \
        if "portal" in o else ""
    if not m:
        return out or [f"The rules table {r['table']} names no pratica for this case."], []
    labels = {x if isinstance(x, str) else x.get("leaf") for w in steps.get("wizard", []) for x in w.get("tree", [])}
    seen = m.get("portal", {})
    if m["procedure"] != steps["procedure"] or any(seen[k] not in labels for k in ("settore", "operazione") if k in seen):
        out.append(f"This case is another pratica: {m['procedure']} ({m['regime']}){where(m)}.")
    handles = r.get("handles", [])
    for a in d["also"]:
        if a["procedure"] not in handles:
            out.append(f"This case also needs {a['procedure']} ({a['regime']}), which this pratica doesn't file.")
    for h in handles:
        if h not in [a["procedure"] for a in d["also"]]:
            out.append(f"This pratica also files {h}, which this case doesn't need.")
    return out, []


def supported(steps, values):
    """Why this case isn't mapped, and the asks still needed to tell."""
    out, waiting = ruled(steps, values)
    need = []
    for f in waiting:
        tmpl = steps["rules"]["facts"][f]
        asked = [r for r, _ in refs_in(tmpl) if r.startswith("ask.")]
        need += asked
        if not asked:  # read from the vault, and empty there
            out.append(f"The rules need {f}, read from {tmpl}: empty in the vault.")
    for c in steps.get("supports", []):
        tmpl = c.get("when", c.get("value"))
        unknown = [r for r, f in refs_in(tmpl) if r.startswith("ask.") and values.field(r) is None]
        if unknown:
            need += unknown
            continue
        v = values.render(tmpl)
        ok = v is not None and (truthy(v) if "when" in c else v in [str(x) for x in c["in"]])
        if not ok:
            out.append(values.render(c["else"]) or c["else"])
    return out, need


def require_supported(steps, values):
    out, need = supported(steps, values)
    if out:
        raise StepError("not supported: " + "; ".join(out))
    if need:
        raise StepError("unanswered: " + ", ".join(sorted(set(need))) + " (run preflight)")


def check_answer(ask, v):
    """Why an answer doesn't fit its ask, or None."""
    s = str(v).strip()
    kind = ask["type"]
    if kind == "yes_no" and not (isinstance(v, bool) or s.lower() in ("true", "false", "si", "sì", "no", "yes")):
        return "answer yes or no"
    if kind == "number":
        try:
            float(s.replace(",", "."))
        except ValueError:
            return "answer with a number"
    if kind == "date":
        try:
            date.fromisoformat(s)
        except ValueError:
            return "answer with a date (YYYY-MM-DD)"
    if kind == "choice" and s not in [str(o) for o in ask["options"]]:
        return "answer with one of: " + ", ".join(map(str, ask["options"]))
    if kind == "file" and not Path(s).expanduser().is_file():
        return f"no file {s}"
    if kind == "text" and not s:
        return "the answer is empty"
    return None


def portal_changed(steps):
    """A generated steps file whose pratica the last drift check (catalogue.py drift) found
    changed on the portal: refused until it's crawled and generated again."""
    m = re.match(r"gen\.py from (\S+) ", steps.get("generated", ""))
    if not m:
        return []
    import catalogue  # noqa: E402 (portal/, on the path below)
    diff = catalogue.drifted(m.group(1))
    return [f"The portal changed this pratica since it was mapped ({'; '.join(diff)}): it needs a new crawl "
            "(crawl.js) and steps file (gen.py) first."] if diff else []


def preflight(steps, values, urls=None):
    """Everything a run needs, settled before any browser opens: why this case isn't
    supported, the questions still to ask, the vault fields missing or expired, the files
    to attach, and the person's own steps. ready: nothing is left before the run. urls: the
    files sent by link (page --file-urls), which don't count to the pane's 256 KB."""
    links = file_links(urls)
    order = check_asks(steps)
    asks = steps.get("asks", {})
    report = {"ready": False, "unsupported": [], "proposals": [], "questions": [], "missing": [], "files": [],
              "yours": []}
    if any(r.startswith("t:") for _, t, _ in templates(steps) for r, _ in refs_in(t)):
        try:
            values.titolare
        except StepError as e:  # nobody is titolare yet: a missing field like any other
            report["missing"].append({"field": "*:ruolo.tipo", "why": str(e)})
            return report
    report["unsupported"], _ = supported(steps, values)
    report["unsupported"] += portal_changed(steps)
    if report["unsupported"]:
        return report
    need, groups, lists = needed(steps, values), steps.get("groups", {}), {}
    for k in order:
        if k not in need:
            continue
        a, v = asks[k], values.raw(k)
        if v is None and "default" in a:
            continue
        hangs = {"only_if": sorted(need[k])} if need[k] else {}
        if v is None and "propose" in a:
            # What the rules or the vault say it should be: the person confirms it, or says
            # otherwise, instead of answering from scratch. Never used until confirmed.
            proposal = values.render(a["propose"])
            if proposal is not None and not check_answer(a, proposal):
                report["proposals"].append({"key": k, "question": a["question"], "proposal": proposal}
                                           | {x: a[x] for x in ("why",) if x in a} | hangs)
                continue
        why = "not answered" if v is None else check_answer(a, v)
        if not why:
            continue
        if "group" in a:
            g = a["group"]
            if g not in lists:
                lists[g] = {"key": g, "type": "checklist", "question": values.render(groups[g]["question"])
                            or groups[g]["question"], "items": []}
                report["questions"].append(lists[g])
            lists[g]["items"].append({"key": k, "item": a["item"]} | {x: a[x] for x in ("how", "why") if x in a} | hangs)
            continue
        report["questions"].append({"key": k, "type": a["type"], "question": a["question"]}
                                   | {x: a[x] for x in ("options", "labels", "how", "why") if x in a} | hangs
                                   | ({"error": why} if v is not None else {}))
    for g in groups:
        ticked = values.answers.get(g)
        members = [k for k, a in asks.items() if a.get("group") == g]
        if ticked is not None and (not isinstance(ticked, list) or set(ticked) - set(members)):
            if g not in lists:  # every item looked answered, but by a list that isn't one
                lists[g] = {"key": g, "type": "checklist", "question": values.render(groups[g]["question"])
                            or groups[g]["question"], "items": [{"key": k, "item": asks[k]["item"]} for k in members]}
                report["questions"].insert(0, lists[g])
            lists[g]["error"] = "answer with a list of the keys of the items that apply ([] for none)"
    missing, read = {}, set()
    for where, tmpl, f in templates(steps):
        if where in ("supports", "rules", "yours") or (f.get("optional") and tmpl == f.get("value")):
            continue
        if f and tmpl == f.get("value") and "when" in f:
            w = values.render(f["when"])
            if w is not None and not truthy(w):
                continue  # this field isn't filled for this case
        for ref, filters in refs_in(tmpl):
            if ref.startswith("ask."):
                continue
            full = values.titolare + ":" + ref[2:] if ref.startswith("t:") else ref
            read.add(full.rsplit(".", 1)[0])
            if values.field(ref) is None and "empty" not in filters:
                missing.setdefault(full, {"field": full, "why": "empty in the vault", "for": where})
    today = date.today().isoformat()
    for cat in sorted(read):
        end = values.category(cat).get(cat + ".data_scadenza")
        if end and end < today:
            missing.setdefault(cat + ".data_scadenza", {"field": cat + ".data_scadenza", "why": f"expired on {end}",
                                                       "for": cat})
    # A value longer than its box (a protocol "PG/2026/0666666" where the shop's form takes 10)
    # is asked again, or named as the vault field to shorten, before the run instead of by it.
    asked = {q["key"] for q in report["questions"]}
    for sec in steps["sections"]:
        for f in sec["fields"]:
            if not f.get("max") or "value" not in f or ("when" in f and not truthy(values.render(f["when"]) or "")):
                continue
            v = values.render(f["value"])
            if v is None or len(v) <= f["max"]:
                continue
            why = f"{sec['section']}: {f.get('label') or f.get('id')} takes at most {f['max']} characters"
            refs = refs_in(f["value"])
            if len(refs) == 1 and refs[0][0].startswith("ask.") and refs[0][0][4:] in asks:
                k = refs[0][0][4:]
                if k not in asked:
                    asked.add(k)
                    report["questions"].append({"key": k, "type": asks[k]["type"], "question": asks[k]["question"],
                                                "error": why})
            else:
                missing.setdefault(f["value"], {"field": f["value"], "why": why, "for": sec["section"]})
    report["missing"] = list(missing.values())
    for sec in steps["sections"]:
        for f in sec["fields"]:
            if f["kind"] == "file":
                if "when" in f and any(v is not None and not truthy(v) for v in
                                       map(values.render, re.findall(r"\{\{.+?\}\}", f["when"]))):
                    continue  # not attached in this case
                path = values.render(f.get("value", ""))
                p = Path(path).expanduser() if path else None
                entry = {"for": f"{sec['section']}: {f.get('label') or f.get('id')}", "path": path,
                         "exists": bool(p and p.is_file()), "bytes": p.stat().st_size if p and p.is_file() else None}
                if p and p.resolve() in links:
                    entry["link"] = links[p.resolve()]
                if entry["exists"]:
                    takes = [t for t in f.get("accepts", "").lower().split(";") if t]
                    if entry["bytes"] > SLOT_MAX_BYTES:
                        entry["problem"] = (f"{entry['bytes'] / 1024 / 1024:.1f} MB, over the portal's 9.77 MB a file: "
                                            "compress it or split it across the slot's +")
                    elif takes and not any(p.name.lower().endswith("." + t) for t in takes):
                        entry["problem"] = f"the slot takes only {', '.join(takes)}"
                report["files"].append(entry)
    report["pane_ok"] = sum(x["bytes"] or 0 for x in report["files"] if "link" not in x) <= PAGE_FILES_LIMIT
    report["yours"] = [values.render(y) or y for y in steps.get("yours", [])]
    report["ready"] = not (report["questions"] or report["proposals"] or report["missing"]
                           or any(not x["exists"] or x.get("problem") for x in report["files"] if x["path"]))
    return report


def attachment(path, url=None):
    """A file as run.js carries it to inpage.js: its bytes, or a link to them with the size
    and sha256 the page holds what it fetches to."""
    p = Path(path).expanduser()
    kind = FILE_TYPES.get(p.suffix.lower()) or mimetypes.guess_type(p.name)[0] or "application/octet-stream"
    data = p.read_bytes()
    if url:
        return {"name": p.name, "type": kind, "url": url, "size": len(data), "sha256": hashlib.sha256(data).hexdigest()}
    return {"name": p.name, "type": kind, "b64": base64.b64encode(data).decode()}


# A file's link is fetched by the portal's page: https only (loopback for the bench).
FILE_URL = re.compile(r"https://|http://(127\.0\.0\.1|localhost)[:/]")


def file_links(urls):
    """{path: URL} keyed by the file each path names, every URL checked."""
    out = {}
    for path, url in (urls or {}).items():
        if not FILE_URL.match(str(url)):
            raise StepError(f"file link for {path}: {url!r} isn't https")
        out[Path(path).expanduser().resolve()] = url
    return out


def resumption(report, fixes=None):
    """How run.js picks up a stopped run: the draft it stopped on, the section it stopped
    in (redone, with any fixes), and what the earlier sections typed, for the fresh load.
    report is report.json plus `kept` from private/kept.json: a report has no values."""
    at = report.get("at") or {}
    if not report.get("url") or not at:
        raise StepError("this report has no place to resume from")
    if at.get("phase") == "section" and at.get("index") and not report.get("kept"):
        raise StepError("no record of what the earlier sections typed (private/kept.json next to report.json): "
                        "resume in the pane's tab with __pv.resume(), or run again from the start")
    return {"url": report["url"], "from": {("section" if at.get("phase") == "section" else "wizard"): at["index"]},
            "kept": report.get("kept", []), "fixes": fixes or []}


def page_scripts(steps, values, files_limit=PAGE_FILES_LIMIT, resume=None, urls=None, code=None):
    """install.js (inpage.js and the steps, no personal data) and run.js (the values)."""
    files = page_files(steps, values, files_limit, resume, urls, code)
    return files["install.js"], files["run.js"]


def page_files(steps, values, files_limit=PAGE_FILES_LIMIT, resume=None, urls=None, code=None):
    """Every file `run.py page` writes, by name: install.js is runner.js and steps.js, and
    check.js says which of those two the pane's storage lacks."""
    return page_bundle(steps, values, files_limit, resume, urls, code)[0]


# --- sealing run.js ---------------------------------------------------------------
# In the pane Claude writes every byte of the run.js call, so the person's values would pass
# through the conversation. With a code, run.js carries them sealed instead, and the code
# reaches the page through the person, not through Claude: shown in a window Claude has no
# output from (show_code) and typed into a box the runner opens (inpage.js, askCode).
# The code is 15 characters of Crockford base32 (75 bits), stretched by PBKDF2-HMAC-SHA256
# and split into an encryption and a MAC key; the plaintext is XORed with an HMAC-SHA256
# keystream and the result authenticated, encrypt-then-MAC. Only the standard library and
# WebCrypto's PBKDF2 and HMAC, which every page has, so there is nothing to install.
SEAL_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
SEAL_CODE_LENGTH = 15
SEAL_ITERATIONS = 200_000


def new_code():
    return "".join(secrets.choice(SEAL_ALPHABET) for _ in range(SEAL_CODE_LENGTH))


def pretty_code(code):
    return "-".join(code[i:i + 5] for i in range(0, len(code), 5))


def seal_keys(code, salt, iterations):
    master = hashlib.pbkdf2_hmac("sha256", code.encode(), salt, iterations, 32)
    return hmac.new(master, b"pv-enc", "sha256").digest(), hmac.new(master, b"pv-mac", "sha256").digest()


def seal_stream(key, nonce, n):
    out = b"".join(hmac.new(key, nonce + i.to_bytes(4, "big"), "sha256").digest() for i in range((n + 31) // 32))
    return out[:n]


def seal(plain, code, iterations=SEAL_ITERATIONS):
    """plain (bytes) sealed under code: {v, i, s, n, c, t}, each binary part in base64."""
    salt, nonce = os.urandom(16), os.urandom(16)
    enc, mac = seal_keys(code, salt, iterations)
    ct = (int.from_bytes(plain, "big") ^ int.from_bytes(seal_stream(enc, nonce, len(plain)), "big")).to_bytes(len(plain), "big")
    tag = hmac.new(mac, b"pv1" + iterations.to_bytes(4, "big") + salt + nonce + ct, "sha256").digest()
    b = lambda x: base64.b64encode(x).decode()
    return {"v": 1, "i": iterations, "s": b(salt), "n": b(nonce), "c": b(ct), "t": b(tag)}


def unseal(sealed, code):
    """The inverse of seal, for tests: raises StepError for a wrong code."""
    d = lambda k: base64.b64decode(sealed[k])
    enc, mac = seal_keys(code, d("s"), sealed["i"])
    ct = d("c")
    want = hmac.new(mac, b"pv1" + sealed["i"].to_bytes(4, "big") + d("s") + d("n") + ct, "sha256").digest()
    if not hmac.compare_digest(want, d("t")):
        raise StepError("that code doesn't open it")
    return (int.from_bytes(ct, "big") ^ int.from_bytes(seal_stream(enc, d("n"), len(ct)), "big")).to_bytes(len(ct), "big")


def show_code(code, give_up=None):
    """The code in a window of its own, for the person: it must not go through the output of
    anything Claude runs, so it is not printed, not in a file, and not in the child's arguments
    or environment (a program's arguments can be listed): the child reads it on stdin. The
    window stays until they close it, so run.py can return. give_up (tests): seconds."""
    text = (f"Type this code into the portal page when it asks for it:\n\n{pretty_code(code)}\n\n"
            "Keep this window open until the run is done. Don't tell it to anyone, Claude included.")
    if sys.platform == "darwin":
        script = (f'display dialog {json.dumps(text)} with title "pvault" buttons {{"Done"}} default button "Done"'
                  + (f" giving up after {give_up}" if give_up else ""))
        cmd, feed = ["osascript", "-"], script
    elif sys.platform == "win32":
        cmd = [sys.executable, "-I", "-c",
               "import ctypes, sys; ctypes.windll.user32.MessageBoxW(0, sys.stdin.read(), 'pvault', 0x40 | 0x40000)"]
        feed = text
    elif shutil.which("zenity"):
        cmd, feed = ["zenity", "--text-info", "--title=pvault", "--width=420", "--height=200"], text
    else:
        raise StepError("no way to show the code outside this conversation here: use `run` (its window "
                        "needs no code), or --plain if the values may pass through the conversation")
    child = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                             text=True, start_new_session=True)
    child.stdin.write(feed)
    child.stdin.close()
    for _ in range(10):  # a window that can't open (no desktop session) fails at once
        time.sleep(0.1)
        if child.poll() is not None:
            break
    if child.poll() not in (None, 0):
        raise StepError("the code window didn't open: use `run`, or --plain if the values may pass through the conversation")
    return child


def page_bundle(steps, values, files_limit=PAGE_FILES_LIMIT, resume=None, urls=None, code=None):
    """page_files, and what runner.js and steps.js leave in the pane's localStorage. urls:
    {path: https URL}, files the page fetches itself instead of run.js carrying them.
    code: seal the values under it (see above); None carries them in the clear."""
    links = file_links(urls)
    require_supported(steps, values)
    plan(steps, values)  # stops on a missing required value
    skeleton = {
        "start": steps["start"], "logged_in": steps["logged_in"],
        "wizard": [{k: v for k, v in w.items() if k != "fill"} | (
            {"fill": {"css": w["fill"]["css"]}} if "fill" in w else {}) for w in steps["wizard"]],
        "sections": [{"section": s["section"], "open": s.get("open", True)}
                     | ({"save": s["save"]} if "save" in s else {})
                     | ({"theirs": s["theirs"]} if "theirs" in s else {})
                     | ({"go": s["go"]} if "go" in s else {})
                     | {"fields": [{k: f[k] for k in ("id", "css", "kind", "label", "anchor", "block", "nth", "describe", "if_present") if k in f}
                                   for f in s["fields"]]}
                     for s in steps["sections"]],
        "forbidden": FORBIDDEN.pattern, "declarations": DECLARATIONS.pattern,
        "dug": sorted(DUG), "label_js": LABEL_JS,
    }

    def value(f):
        """False when its `when` is false, true for a tick, a file's bytes, else the value."""
        if "when" in f and not truthy(values.render(f["when"]) or ""):
            return False
        if "value" not in f:
            return True
        v = values.render(f["value"])
        if f["kind"] == "file" and v:
            return attachment(v, links.get(Path(v).expanduser().resolve()))
        return v

    vals = {
        "wizard": [values.render(w["fill"]["value"]) if "fill" in w else None for w in steps["wizard"]],
        "sections": [[value(f) for f in s["fields"]] for s in steps["sections"]],
    }
    if files_limit is not None:
        files = [v for s in vals["sections"] for v in s if isinstance(v, dict) and "b64" in v]
        size = sum(len(f["b64"]) * 3 // 4 for f in files)
        if size > files_limit:
            raise StepError(f"the attachments come to {size // 1024} KB, more than the {files_limit // 1024} KB "
                            "run.js can carry in the pane: send the big ones by link (--file-urls) or use `run`")
    proc = steps["procedure"]
    runner_js, steps_js, stored = pane_parts(proc, skeleton)
    load = "if (!window.__pv) (0, eval)(localStorage.getItem('pv-runner'));\n"
    if code:
        plain = json.dumps({"vals": vals, "resume": resume or {}}, ensure_ascii=False, separators=(",", ":"))
        run_js = load + f"__pv.startSealed({json.dumps(proc)}, {json.dumps(seal(plain.encode(), code), separators=(',', ':'))})\n"
    else:
        run_js = (load + f"__pv.start({json.dumps(proc)}, {json.dumps(vals, ensure_ascii=False, separators=(',', ':'))}"
                  + (f", {json.dumps(resume, ensure_ascii=False, separators=(',', ':'))}" if resume else "") + ")\n")
    origin = re.match(r"https?://[^/]+", steps["logged_in"])[0]
    return {"install.js": runner_js + steps_js + "'installed'\n", "runner.js": runner_js, "steps.js": steps_js,
            "check.js": check_script(stored, origin), "run.js": run_js}, stored


def pane_parts(proc, skeleton):
    """runner.js (inpage.js) and steps.js (the steps, no personal data) as install.js holds
    them, and what each leaves in the pane's localStorage, for check.js."""
    runner = Path(__file__).with_name("inpage.js").read_text()
    steps_json = json.dumps(skeleton, ensure_ascii=False, separators=(',', ':'))
    # A function's source is its text, so neither part needs escaping into a string.
    # The runner a page already loaded stays loaded: drop it, so run.js loads this one.
    runner_js = f"localStorage.setItem('pv-runner', '(' + function () {{\n{runner}}} + ')()');\ndelete window.__pv;\n"
    steps_js = f"localStorage.setItem({json.dumps('pv-steps:' + proc)}, JSON.stringify({steps_json}));\n"
    # JSON.stringify of that literal gives back these same bytes; the runner is stored as its source.
    stored = {"pv-runner": "(function () {\n" + runner + "})()", "pv-steps:" + proc: steps_json}
    return runner_js, steps_js, stored


def check_script(stored, origin, files=None, then="run.js"):
    """check.js: on the portal's page, which of runner.js and the steps the pane must still run
    (its localStorage holds something else), and whether the page is on the portal's origin,
    the only one whose storage has them. The rest of install.js need not be sent again.
    files: the file that writes each key (default runner.js, steps.js); then: what follows."""
    want = {k: hashlib.sha256(v.encode()).hexdigest() for k, v in stored.items()}
    files = files or {k: "runner.js" if k == "pv-runner" else "steps.js" for k in stored}
    return ("(async () => {\n"
            "  const sha = async (s) => s === null ? null : Array.from(new Uint8Array(await crypto.subtle.digest(\"SHA-256\", new TextEncoder().encode(s)))).map((b) => b.toString(16).padStart(2, \"0\")).join(\"\");\n"
            f"  const want = {json.dumps(want)};\n"
            "  const stale = [];\n"
            "  for (const [k, v] of Object.entries(want)) if (await sha(localStorage.getItem(k)) !== v) stale.push(k);\n"
            f"  if (location.origin !== {json.dumps(origin)}) return {json.dumps('go to ' + origin + ' first: its storage holds the runner')};\n"
            f"  const files = {json.dumps(files)};\n"
            f"  return \"run \" + stale.map((k) => files[k]).concat([{json.dumps(then)}]).join(\", then \");\n"
            "})()\n")




# --- browser ----------------------------------------------------------------------

LABEL_JS = """(el) => {
  const texts = [];
  const t = el.closest('[title]'); if (t) texts.push(t.title);
  const td = el.closest('td'); if (td) texts.push(td.innerText);
  // A dynaForm puts a label in the row above, in the same column, or to the left.
  for (let tr = td && td.parentElement.previousElementSibling; tr; tr = tr.previousElementSibling) {
    const c = tr.cells[td.cellIndex];
    if (c && c.innerText.trim()) { texts.push(c.innerText); break; }
  }
  for (let c = td && td.previousElementSibling; c; c = c.previousElementSibling) {
    if (c.innerText.trim()) { texts.push(c.innerText); break; }
  }
  const labels = [...document.querySelectorAll('.labelDynaFormControl, label')];
  const before = labels.filter(l => l.compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING && !l.contains(el));
  if (before.length) texts.push(before[before.length - 1].innerText);
  return texts.map(s => (s || '').replace(/\\s+/g, ' ').trim());
}"""


def drift(out, steps, resume):
    """What a recovered run changed, for the steps file and the map: never silent."""
    if resume and resume["fixes"]:
        (out / "drift.json").write_text(json.dumps({"procedure": steps["procedure"], "fixes": resume["fixes"],
                                                    "draft": resume["url"]}, indent=2, ensure_ascii=False))


def review_shown(steps, values):
    """What the review page shows besides the run: each value, and the person's own steps."""
    return {"title": steps.get("title"), "yours": [values.render(y) for y in steps.get("yours", [])],
            "values": [{"section": v["section"], "label": v["label"] or v["id"],
                        "value": "chosen" if v["value"] is None
                        else "attached: " + Path(v["value"]).name if v["kind"] == "file" else v["value"]}
                       for v in plan(steps, values)]}


def private(out, shown, held=None):
    """What no report holds: what each section typed (to resume), what a failed read-back wanted
    and found, and every value for the review page. A folder only the person reads; the skill
    tells Claude to send the review page and never to open these."""
    folder = out / "private"
    folder.mkdir(mode=0o700, exist_ok=True)
    folder.chmod(0o700)
    parts = [("shown", shown)]
    if held is not None:  # a run in the pane has only shown: __pv.held() stays in its tab
        parts += [("kept", held.get("kept", [])), ("held", held.get("details", []))]
    for name, data in parts:
        f = folder / f"{name}.json"
        f.write_text(json.dumps(data, indent=2, ensure_ascii=False))
        f.chmod(0o600)


def run(steps, values, out, cdp=None, profile=None, close=False, resume=None):
    """inpage.js in a Playwright window: the runner the Claude browser pane uses, without
    the pane's 45 s call limit and slowed timers. A window behind others keeps full speed
    with no call of ours: Playwright turns on focus emulation for each page it drives
    (--cdp too) and launches Chromium with background throttling off."""
    from playwright.sync_api import sync_playwright
    # page.evaluate runs run.js here, not through the conversation, so files of any size.
    install, run_js = page_scripts(steps, values, files_limit=None, resume=resume)
    out.mkdir(parents=True, exist_ok=True)
    drift(out, steps, resume)
    shown = review_shown(steps, values)
    report = {"procedure": steps["procedure"], "result": "stopped"}
    held = {}
    with sync_playwright() as pw:
        if cdp:
            ctx = pw.chromium.connect_over_cdp(cdp).contexts[0]
        else:
            try:
                ctx = pw.chromium.launch_persistent_context(str(profile), headless=False, no_viewport=True)
            except Exception as e:
                if "already in use" not in str(e) and "existing browser session" not in str(e):
                    raise
                # A window an earlier run left open still has the profile, and with it the login.
                raise StepError(f"another Chrome for Testing window is using {profile}: close it (⌘Q) and run "
                                "again, or pass --profile with another folder (and log in there again)") from None
        page = ctx.pages[0] if ctx.pages else ctx.new_page()
        try:
            page.goto(steps["logged_in"] if resume else steps["start"])  # a resume mustn't start a new pratica
            if not page.url.startswith(steps["logged_in"]):
                print("Log in with SPID in the Playwright window; the run starts when you're in.", flush=True)
                page.wait_for_url(lambda u: u.startswith(steps["logged_in"]), timeout=15 * 60 * 1000)
            page.wait_for_load_state()
            page.evaluate(install)
            page.evaluate(run_js)
            wizard_shown, sections_shown = False, 0
            while True:
                report = page.evaluate("__pv.wait(5000)")
                if not wizard_shown and report.get("wizard_seconds") is not None:
                    print(f"  wizard: {report['wizard_seconds']} s", flush=True)
                    wizard_shown = True
                for sec in report["sections"][sections_shown:]:
                    print(f"  {sec['section']}: {sec['seconds']} s", flush=True)
                sections_shown = len(report["sections"])
                if report["result"] != "running":
                    if report.get("verify_seconds") is not None:
                        print(f"  fresh load of the draft, every value read again: {report['verify_seconds']} s",
                              flush=True)
                    break
        except Exception as e:
            report["result"] = "stopped"
            report["error"] = f"{type(e).__name__}: {e}"
        finally:
            if not page.is_closed():
                try:
                    held = page.evaluate("__pv.held()")
                except Exception:
                    pass
            # report.json holds no value, so Claude may read it; what the person needs is
            # kept apart and goes to them, never into the conversation.
            (out / "report.json").write_text(json.dumps(report, indent=2, ensure_ascii=False))
            private(out, shown, held)
            (out / "review.html").write_text(review.portal_review(report | shown | {"details": held.get("details", [])}),
                                             encoding="utf-8")
            (out / "review.html").chmod(0o600)
            print(f"review: {out / 'review.html'}", flush=True)
            print(json.dumps({k: report.get(k) for k in ("result", "error", "wizard_seconds", "total_seconds")}),
                  flush=True)
            if not close and not page.is_closed():
                if report["result"] == "filled":
                    page.evaluate("__pv.open()")
                print("Left open for you: tick the declarations, attach what's left, sign and pay yourself. "
                      "Close the window when done.", flush=True)
                page.wait_for_event("close", timeout=0)
            try:
                ctx.close()
            except Exception:
                pass
    return 0 if report["result"] == "filled" else 1


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("command", choices=["preflight", "plan", "page", "run"])
    ap.add_argument("--steps", required=True, type=Path)
    ap.add_argument("--answers", type=Path, help="the asks' answers as JSON (none yet: preflight lists them all)")
    src = ap.add_mutually_exclusive_group(required=True)
    src.add_argument("--server", type=Path)
    src.add_argument("--vault-dir")
    src.add_argument("--values", help="- or a file: pvault get's JSON of the fields the steps read")
    ap.add_argument("--token", default="prepare", help="--server: which of its tokens")
    ap.add_argument("--keychain", help="--vault-dir: the Keychain service of the token")
    ap.add_argument("--out", type=Path)
    ap.add_argument("--cdp")
    ap.add_argument("--profile", type=Path, default=Path.home() / ".cache/pratica-portal/profile")
    ap.add_argument("--close", action="store_true")
    ap.add_argument("--plain", action="store_true", help="page: run.js carries the values in the clear, through the conversation")
    ap.add_argument("--resume", type=Path, help="page/run: a stopped run's report.json, to pick it up where it stopped")
    ap.add_argument("--file-urls", type=Path, help="preflight/page: JSON {path: https URL}, files the page fetches by link")
    ap.add_argument("--fix", type=Path, help="with --resume: JSON list of {section, field, set: {id|label|css}}")
    a = ap.parse_args()
    steps = yaml.safe_load(a.steps.read_text())
    try:
        answers = json.loads(a.answers.read_text()) if a.answers else {}
        asks = steps.get("asks")
        if a.values:
            given = sys.stdin.read() if a.values == "-" else Path(a.values).read_text()
            if not given.strip():
                raise StepError("no values: pvault get printed nothing (its error is above)")
            values = Values(answers, fields=json.loads(given), asks=asks)
        else:
            vaults = fill.connect(argparse.Namespace(server=a.server, vault_dir=a.vault_dir, keychain=a.keychain))
            if a.server and a.token not in vaults:
                ap.error(f"{a.server} has no token {a.token!r}")
            values = Values(answers, vault=vaults[a.token if a.server else "prepare"], asks=asks)
        resume = None
        if a.resume:
            earlier = json.loads(a.resume.read_text())
            kept = a.resume.parent / "private" / "kept.json"
            if kept.exists():
                earlier["kept"] = json.loads(kept.read_text())
            resume = resumption(earlier, json.loads(a.fix.read_text()) if a.fix else None)
        if a.fix and not a.resume:
            ap.error("--fix needs --resume")
        if a.plain and a.command != "page":
            ap.error("--plain is for page: run keeps the values out of the conversation without it")
        if a.command == "preflight":
            report = preflight(steps, values, json.loads(a.file_urls.read_text()) if a.file_urls else None)
            print(json.dumps(report, indent=2, ensure_ascii=False))
            return 0 if report["ready"] else 3
        if a.command == "plan":
            require_supported(steps, values)
            print(f"titolare: {values.titolare}")
            for p in plan(steps, values):
                print(f"{p['section'][:28]:28}  {(p['label'] or p['id'] or p.get('css') or '')[:34]:34}  {p['value'] if p['value'] is not None else '(' + p['kind'] + ')'}")
            return 0
        if not a.out:
            ap.error(f"{a.command} needs --out")
        if a.command == "page":
            a.out.mkdir(parents=True, exist_ok=True)
            urls = json.loads(a.file_urls.read_text()) if a.file_urls else None
            code = None if a.plain else new_code()
            files = page_files(steps, values, resume=resume, urls=urls, code=code)
            drift(a.out, steps, resume)
            for name, text in files.items():
                (a.out / name).write_text(text)
            # The pane's report has no values or person's steps: review.py OUT adds these, from
            # private/ (Claude sends the review page and never opens that folder).
            private(a.out, review_shown(steps, values))
            if code:
                try:
                    show_code(code)
                except StepError:
                    (a.out / "run.js").unlink(missing_ok=True)
                    raise
                print("run.js is sealed. A window with its code is open on the person's screen: tell them it is "
                      "for the box the portal's page will show when run.js runs, to leave the window open until "
                      "the run is done, and never to tell you the code.")
            else:
                print("--plain: run.js carries the values in the clear, so they pass through this conversation.")
            print(f"{a.out}: check.js first (which of runner.js, steps.js the pane lacks), then run.js; "
                  f"install.js is both. Save __pv.last() as report.json here, then review.py {a.out}. "
                  + ", ".join(f"{n} {len(t)} bytes" for n, t in files.items()))
            return 0
        return run(steps, values, a.out, a.cdp, a.profile, a.close, resume)
    except StepError as e:
        print(f"stopped: {e}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
