#!/usr/bin/env python3
"""Fill a portal pratica from the vault in a real browser, then hand it to the person.

  run.py plan --steps STEPS.yaml --answers A.json
              (--values - | --server S.json --token NAME | --vault-dir DIR [--keychain SERVICE])
      Resolves every value the steps need and prints what goes where. No browser.

  run.py page --steps STEPS.yaml --answers A.json VAULT --out DIR
      Writes DIR/install.js and DIR/run.js for the Claude browser pane, where each runs
      with one call on the portal's page: install.js stores inpage.js and the steps (no
      personal data), run.js starts a run with the person's values. inpage.js says more.

  run.py run  --steps STEPS.yaml --answers A.json VAULT --out DIR
              [--cdp URL | --profile DIR] [--close]
      Opens the portal in a Playwright window (its own Chromium) and waits for the person
      to log in, then runs inpage.js there, as in the pane: the wizard, each section filled,
      saved and read back, then the draft loaded again and every value read once more. It
      stops before what is the person's: the declarations, the attachments the steps file
      doesn't name, the payment, signing and sending, and shows the draft, left open for
      them unless --close. Writes DIR/report.json (timings, what each section kept),
      DIR/review.html (the page the person reviews: what's left for them, the steps file's
      `yours`, and every value) and DIR/failure.png when a step fails.

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
itself and takes any size.

No model is called during a run. A step that doesn't match what the steps file expects
(a label, a lookup with no single answer, a value the portal didn't keep) stops the run:
that is the point where Claude looks at the page and fixes the steps file.
"""

import argparse
import base64
import json
import mimetypes
import re
import sys
from pathlib import Path

import yaml

sys.path[:0] = [str(Path(__file__).resolve().parent.parent / "fill"), str(Path(__file__).resolve().parent.parent)]
sys.dont_write_bytecode = True  # no __pycache__ in the skill's folder
import fill  # noqa: E402  (its vault client: the pilot over TCP, a real vault over its socket)
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


FILTERS = {
    "upper": str.upper,
    "date_it": lambda v: "/".join(reversed(v.split("-"))),
    "phone_it": lambda v: re.sub(r"^\+39|\s", "", v),
    "dug": lambda v: _split_street(v)[0],
    "street": lambda v: _split_street(v)[1],
    "carica": _carica,
    "forma": lambda v: FORMA[v],
    "categoria": lambda v: re.sub(r"^([A-F])/?(\d)$", r"\g<1>0\2", v.replace(" ", "").upper()),
    "empty": lambda v: "true" if not v else "false",
    "not": lambda v: "false" if truthy(v) else "true",
}


def truthy(v):
    return str(v).strip().lower() in ("true", "1", "yes", "si", "sì")


class StepError(Exception):
    pass


# --- values -----------------------------------------------------------------------

class Values:
    """The vault fields and answers a steps file refers to."""

    def __init__(self, answers, vault=None, fields=None):
        """From the vault with a token, or from fields already read (pvault get's JSON)."""
        self.vault, self.fields = vault, fields
        self.answers = answers
        self.cache = {}
        self.titolare = self._titolare()

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

    def field(self, ref):
        if ref.startswith("ask."):
            v = self.answers.get(ref[4:])
            return None if v is None else str(v).lower() if isinstance(v, bool) else str(v)
        if ref.startswith("t:"):
            ref = self.titolare + ":" + ref[2:]
        cat = ref.rsplit(".", 1)[0]
        return self.category(cat).get(ref)

    def render(self, template):
        """A template's value, or None when a field it names is missing."""
        def one(m):
            ref, *filters = [p.strip() for p in m.group(1).split("|")]
            v = self.field(ref)
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
            out.append({"section": sec["section"], "id": f.get("id") or f.get("css"), "kind": f["kind"],
                        "label": f.get("label"), "value": v})
    if missing:
        raise StepError("missing values:\n  " + "\n  ".join(missing))
    return out


def attachment(path):
    """A file as run.js carries it to inpage.js."""
    p = Path(path).expanduser()
    kind = FILE_TYPES.get(p.suffix.lower()) or mimetypes.guess_type(p.name)[0] or "application/octet-stream"
    return {"name": p.name, "type": kind, "b64": base64.b64encode(p.read_bytes()).decode()}


def page_scripts(steps, values, files_limit=PAGE_FILES_LIMIT):
    """install.js (inpage.js and the steps, no personal data) and run.js (the values)."""
    plan(steps, values)  # stops on a missing required value
    skeleton = {
        "start": steps["start"], "logged_in": steps["logged_in"],
        "wizard": [{k: v for k, v in w.items() if k != "fill"} | (
            {"fill": {"css": w["fill"]["css"]}} if "fill" in w else {}) for w in steps["wizard"]],
        "sections": [{"section": s["section"], "open": s.get("open", True)}
                     | ({"save": s["save"]} if "save" in s else {})
                     | {"fields": [{k: f[k] for k in ("id", "css", "kind", "label") if k in f} for f in s["fields"]]}
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
        return attachment(v) if f["kind"] == "file" and v else v

    vals = {
        "wizard": [values.render(w["fill"]["value"]) if "fill" in w else None for w in steps["wizard"]],
        "sections": [[value(f) for f in s["fields"]] for s in steps["sections"]],
    }
    if files_limit is not None:
        files = [v for s in vals["sections"] for v in s if isinstance(v, dict)]
        size = sum(len(f["b64"]) * 3 // 4 for f in files)
        if size > files_limit:
            raise StepError(f"the attachments come to {size // 1024} KB, more than the {files_limit // 1024} KB "
                            "run.js can carry in the pane: use `run` (a Playwright window) or attach them by hand")
    proc = steps["procedure"]
    runner = Path(__file__).with_name("inpage.js").read_text()
    # A function's source is its text, so neither part needs escaping into a string.
    install = (f"localStorage.setItem('pv-runner', '(' + function () {{\n{runner}}} + ')()');\n"
               f"localStorage.setItem({json.dumps('pv-steps:' + proc)}, JSON.stringify("
               f"{json.dumps(skeleton, ensure_ascii=False, separators=(',', ':'))}));\n"
               "'installed'\n")
    run_js = ("if (!window.__pv) (0, eval)(localStorage.getItem('pv-runner'));\n"
              f"__pv.start({json.dumps(proc)}, {json.dumps(vals, ensure_ascii=False, separators=(',', ':'))})\n")
    return install, run_js


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


def run(steps, values, out, cdp=None, profile=None, close=False):
    """inpage.js in a Playwright window: the runner the Claude browser pane uses, without
    the pane's 45 s call limit and slowed timers. A window behind others keeps full speed
    with no call of ours: Playwright turns on focus emulation for each page it drives
    (--cdp too) and launches Chromium with background throttling off."""
    from playwright.sync_api import sync_playwright
    # page.evaluate runs run.js here, not through the conversation, so files of any size.
    install, run_js = page_scripts(steps, values, files_limit=None)
    out.mkdir(parents=True, exist_ok=True)
    # What the review page shows besides the run: each value, and the person's own steps.
    shown = {"title": steps.get("title"), "yours": [values.render(y) for y in steps.get("yours", [])],
             "values": [{"section": v["section"], "label": v["label"] or v["id"],
                         "value": "chosen" if v["value"] is None
                         else "attached: " + Path(v["value"]).name if v["kind"] == "file" else v["value"]}
                        for v in plan(steps, values)]}
    report = {"procedure": steps["procedure"], "result": "stopped"}
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
            page.goto(steps["start"])
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
            if report["result"] != "filled" and not page.is_closed():
                page.screenshot(path=str(out / "failure.png"), full_page=True)
            report |= shown
            (out / "report.json").write_text(json.dumps(report, indent=2, ensure_ascii=False))
            (out / "review.html").write_text(review.portal_review(report), encoding="utf-8")
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
    ap.add_argument("command", choices=["plan", "page", "run"])
    ap.add_argument("--steps", required=True, type=Path)
    ap.add_argument("--answers", required=True, type=Path)
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
    a = ap.parse_args()
    steps = yaml.safe_load(a.steps.read_text())
    try:
        answers = json.loads(a.answers.read_text())
        if a.values:
            given = sys.stdin.read() if a.values == "-" else Path(a.values).read_text()
            if not given.strip():
                raise StepError("no values: pvault get printed nothing (its error is above)")
            values = Values(answers, fields=json.loads(given))
        else:
            vaults = fill.connect(argparse.Namespace(server=a.server, vault_dir=a.vault_dir, keychain=a.keychain))
            if a.server and a.token not in vaults:
                ap.error(f"{a.server} has no token {a.token!r}")
            values = Values(answers, vault=vaults[a.token if a.server else "prepare"])
        if a.command == "plan":
            print(f"titolare: {values.titolare}")
            for p in plan(steps, values):
                print(f"{p['section'][:28]:28}  {(p['label'] or p['id'])[:34]:34}  {p['value'] if p['value'] is not None else '(' + p['kind'] + ')'}")
            return 0
        if not a.out:
            ap.error(f"{a.command} needs --out")
        if a.command == "page":
            a.out.mkdir(parents=True, exist_ok=True)
            install, run_js = page_scripts(steps, values)
            (a.out / "install.js").write_text(install)
            (a.out / "run.js").write_text(run_js)
            print(f"{a.out / 'install.js'} ({len(install)} bytes), {a.out / 'run.js'} ({len(run_js)} bytes)")
            return 0
        return run(steps, values, a.out, a.cdp, a.profile, a.close)
    except StepError as e:
        print(f"stopped: {e}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
