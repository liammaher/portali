#!/usr/bin/env python3
"""Fill a portal pratica from the vault in a real browser, then hand it to the person.

  run.py plan --steps STEPS.yaml --answers A.json --server S.json --token NAME
      Resolves every value the steps need and prints what goes where. No browser.

  run.py page --steps STEPS.yaml --answers A.json --server S.json --token NAME --out DIR
      Writes DIR/install.js and DIR/run.js for the Claude browser pane, where each runs
      with one call on the portal's page: install.js stores inpage.js and the steps (no
      personal data), run.js starts a run with the person's values. inpage.js says more.

  run.py run  --steps STEPS.yaml --answers A.json --server S.json --token NAME --out DIR
              [--cdp URL | --profile DIR] [--close]
      Opens the portal in Chrome and waits for the person to log in, then walks the
      wizard, fills and saves each section and reads every field back. It stops before
      what is the person's: the declarations, the payment, signing and sending. The
      browser stays open for them unless --close. Writes DIR/report.json (each value,
      where it went, what the portal kept, timings) and DIR/failure.png when a step fails.

STEPS.yaml is a portal's steps file (rehearsals/*/portal-steps.yaml). Values come from
the vault over its HTTP API (--server: a JSON with "url" and "tokens", as the pilot
vault writes it) with the named read token, and from A.json for the asks. --cdp attaches
to a Chrome the person started with --remote-debugging-port, for a site that refuses a
browser Playwright launched; otherwise Chrome starts with its own profile (--profile,
default ~/.cache/pratica-portal/profile), never the person's own.

No model is called during a run. A step that doesn't match what the steps file expects
(a label, a lookup with no single answer, a value the portal didn't keep) stops the run:
that is the point where Claude looks at the page and fixes the steps file.
"""

import argparse
import json
import re
import sys
import time
import unicodedata
import urllib.error
import urllib.request
from pathlib import Path

import yaml

# Never clicked, whatever a steps file says: these are the person's taps.
FORBIDDEN = re.compile(r"pago\s*pa|\binvia\b|\bpaga\b|procedi con il pagamento|firma", re.I)
# Never opened: the dichiarante's own statements.
DECLARATIONS = re.compile(
    r"consapevolezza|informativa sulla privacy|requisiti di o|altre dichiarazioni|"
    r"dichiarazioni ai sensi dell'art\. 6|rispetto sulle tasse|art\. ?75", re.I)

DUG = {"VIA", "VIALE", "PIAZZA", "CORSO", "LARGO"}
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

    def __init__(self, server, token, answers):
        self.url = server["url"].rstrip("/")
        self.token = server["tokens"][token]
        self.answers = answers
        self.cache = {}
        self.titolare = self._titolare()

    def _get(self, path):
        req = urllib.request.Request(self.url + path, headers={"Authorization": "Bearer " + self.token})
        with urllib.request.urlopen(req) as r:
            return json.load(r)

    def category(self, cat):
        if cat not in self.cache:
            try:
                rows = self._get("/vault/fields/category/" + cat)
            except urllib.error.HTTPError as e:
                if e.code != 404:
                    raise
                rows = []
            self.cache[cat] = {r["id"]: r["value"] for r in rows}
        return self.cache[cat]

    def _titolare(self):
        ids = [f["id"] for f in self._get("/vault/fields")]
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
            if "value" in f and v is None and not f.get("optional"):
                missing.append(f"{sec['section']}: {f.get('label', f['id'])} ({f['value']})")
            out.append({"section": sec["section"], "id": f["id"], "kind": f["kind"],
                        "label": f.get("label"), "value": v})
    if missing:
        raise StepError("missing values:\n  " + "\n  ".join(missing))
    return out


def page_scripts(steps, values):
    """install.js (inpage.js and the steps, no personal data) and run.js (the values)."""
    plan(steps, values)  # stops on a missing required value
    skeleton = {
        "start": steps["start"], "logged_in": steps["logged_in"],
        "wizard": [{k: v for k, v in w.items() if k != "fill"} | (
            {"fill": {"css": w["fill"]["css"]}} if "fill" in w else {}) for w in steps["wizard"]],
        "sections": [{"section": s["section"], "open": s.get("open", True),
                      "fields": [{k: f[k] for k in ("id", "kind", "label") if k in f} for f in s["fields"]]}
                     for s in steps["sections"]],
        "forbidden": FORBIDDEN.pattern, "declarations": DECLARATIONS.pattern,
        "dug": sorted(DUG), "label_js": LABEL_JS,
    }
    # One entry per field: false when its `when` is false, true for a tick, else the value.
    vals = {
        "wizard": [values.render(w["fill"]["value"]) if "fill" in w else None for w in steps["wizard"]],
        "sections": [[False if "when" in f and not truthy(values.render(f["when"]) or "")
                      else True if "value" not in f else values.render(f["value"])
                      for f in s["fields"]] for s in steps["sections"]],
    }
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


def norm(s):
    s = unicodedata.normalize("NFKD", s or "").encode("ascii", "ignore").decode()
    return re.sub(r"\s+", " ", s).strip().lower()


class Runner:
    def __init__(self, page, report):
        self.page = page
        self.report = report

    def settle(self):
        try:
            self.page.wait_for_load_state("networkidle", timeout=8000)
        except Exception:
            pass

    def click(self, loc, navigates=True):
        text = " ".join(filter(None, [loc.inner_text(timeout=3000) if loc.evaluate("e => e.tagName") != "INPUT" else "",
                                      loc.get_attribute("value") or "", loc.get_attribute("title") or ""]))
        if FORBIDDEN.search(text):
            raise StepError(f"refused to click {text.strip()!r}: that step is the person's")
        before = self.page.url
        if navigates:
            with self.page.expect_navigation(timeout=30000):
                loc.click()
        else:
            loc.click()
        self.settle()
        return before

    def button(self, text):
        loc = self.page.locator("button:visible, a:visible, input[type=submit]:visible").filter(
            has_text=re.compile(rf"^\s*{re.escape(text)}\s*$", re.I))
        if loc.count() == 0:
            loc = self.page.locator(f"input[type=submit][value='{text}']:visible")
        if loc.count() == 0:
            raise StepError(f"no {text!r} button")
        return loc.first

    def errors(self):
        loc = self.page.locator(".ui-message-error:visible, .ui-messages-error:visible, .ui-growl-item:visible")
        return [norm(loc.nth(i).inner_text()) for i in range(loc.count())]

    # wizard

    def screen(self, name):
        if norm(name) not in norm(self.page.locator("body").inner_text()):
            raise StepError(f"expected the screen {name!r}")

    def tree(self, leaf):
        span = self.page.locator(".ui-tree .tree-content > span").filter(
            has_text=re.compile(rf"^\s*{re.escape(leaf)}\s*$"))
        if span.count() != 1:
            raise StepError(f"tree: {span.count()} nodes named {leaf!r}")
        # Open its ancestors, outermost first, then select it.
        keys = span.evaluate("""s => { const out = []; let li = s.closest('li.ui-treenode');
            while ((li = li.parentElement && li.parentElement.closest('li.ui-treenode')))
              out.unshift(li.dataset.rowkey); return out; }""")
        for k in keys:
            content = self.page.locator(f"li[data-rowkey='{k}'] > .ui-treenode-content")
            if content.get_attribute("aria-expanded") != "true":
                label = content.locator(".ui-treenode-label")
                if label.is_visible():
                    label.click()
                    self.settle()
        span.click()
        self.settle()
        ok = span.evaluate("s => s.closest('li.ui-treenode').classList.contains('ui-treenode-selected')")
        if not ok:
            raise StepError(f"tree: {leaf!r} didn't stay selected")

    def wizard(self, step, values):
        self.screen(step["screen"])
        if "fill" in step:
            loc = self.page.locator(step["fill"]["css"])
            loc.fill(values.render(step["fill"]["value"]))
        leaves = step.get("tree") or []
        for leaf in [leaves] if isinstance(leaves, str) else leaves:
            self.tree(leaf)
        self.click(self.button(step["confirm"]))

    # form sections

    def el(self, fid, suffix=""):
        return self.page.locator(f"[id$='dynaForm:{fid}{suffix}']")

    def check_label(self, loc, label):
        if not label:
            return
        texts = loc.evaluate(LABEL_JS)
        if not any(norm(label) in norm(t) for t in texts):
            raise StepError(f"expected the label {label!r} by this field, found {texts[:3]}")

    def fill_text(self, loc, value):
        loc.click()
        loc.press("ControlOrMeta+a")
        loc.press("Backspace")
        loc.press_sequentially(value, delay=15)
        loc.press("Tab")
        self.settle()

    def fill_select(self, fid, value):
        select = self.el(fid, ":combo_input")
        options = select.evaluate("s => [...s.options].map(o => [o.value, o.text.trim()])")
        idx = next((i for i, (v, t) in enumerate(options) if v == value or t.upper() == value.upper()), None)
        if idx is None:
            raise StepError(f"{fid}: no option {value!r}")
        want, label = options[idx]
        # Clicking an item misses while the panel settles, and the filter matches
        # substrings (napoli finds CASALNUOVO DI NAPOLI first): type into the filter
        # with real key presses, arrow to the exact label, Enter.
        for _ in range(3):
            self.el(fid, ":combo_label").click()
            panel = self.el(fid, ":combo_panel")
            panel.wait_for(state="visible", timeout=5000)
            flt = self.el(fid, ":combo_filter")
            if flt.is_visible():
                flt.click()
                flt.press_sequentially(label, delay=20)
                self.page.wait_for_timeout(500)
                shown = panel.evaluate("""p => [...p.querySelectorAll('li')]
                    .filter(l => l.offsetParent && getComputedStyle(l).display !== 'none')
                    .map(l => [l.textContent.trim().toUpperCase(), l.classList.contains('ui-state-highlight')])""")
                names = [n for n, _ in shown]
                if label.upper() not in names:
                    raise StepError(f"{fid}: filter {label!r} shows {names}")
                lit = next((i for i, (_, h) in enumerate(shown) if h), 0)
                for _ in range(names.index(label.upper()) - lit):
                    flt.press("ArrowDown")
                flt.press("Enter")
            else:
                panel.locator("li").nth(idx).click()
            self.settle()
            if select.input_value() == want:
                return want
        raise StepError(f"{fid}: {value!r} didn't stay selected")

    def fill_lookup(self, fid, value):
        dug, street = _split_street(value)
        words = [w for w in re.split(r"\W+", street.upper()) if len(w) > 2]
        loc = self.el(fid)
        loc.click()
        loc.press("ControlOrMeta+a")
        loc.press("Backspace")
        loc.press_sequentially(max(words, key=len), delay=40)
        panel = self.el(fid.replace("_input", "_panel"))
        panel.locator("li").first.wait_for(state="visible", timeout=10000)
        items = [norm(t).upper() for t in panel.locator("li").all_inner_texts()]
        hits = [t for t in items if t.split()[:1] == [dug] and all(w in t.split() for w in words)]
        if len(hits) != 1:
            raise StepError(f"{fid}: {value!r} matches {hits or 'nothing'} among {items}")
        panel.locator("li").nth(items.index(hits[0])).click()
        self.settle()
        return hits[0]

    def section(self, sec, values, fields):
        t0 = time.monotonic()
        if DECLARATIONS.search(sec["section"]):
            raise StepError(f"refused to open {sec['section']!r}: the person's declarations")
        if sec.get("open", True):
            link = self.page.locator("a.linkmenu").filter(
                has_text=re.compile(rf"^\s*{re.escape(sec['section'])}", re.I)).first
            self.click(link)
        done = []
        for f in fields:
            loc = self.el(f["id"], ":combo_input" if f["kind"] == "select" else "")
            loc.wait_for(state="attached", timeout=10000)
            if f["kind"] == "select":
                self.check_label(self.el(f["id"], ":combo_label"), f["label"])
            else:
                self.check_label(loc, f["label"])
            if f["kind"] in ("radio", "checkbox"):
                loc.check()
                self.settle()
                kept = "checked"
            elif f["value"] in (None, ""):
                continue
            elif f["kind"] == "select":
                kept = self.fill_select(f["id"], f["value"])
            elif f["kind"] == "lookup":
                kept = self.fill_lookup(f["id"], f["value"])
            else:
                self.fill_text(loc, f["value"])
                kept = f["value"]
            done.append({**f, "typed": kept})
        self.click(self.button("Salva"))
        errs = self.errors()
        checks = [self.read_back(f) for f in done]
        self.report["sections"].append({"section": sec["section"], "seconds": round(time.monotonic() - t0, 1),
                                        "errors": errs, "fields": checks})
        bad = [c for c in checks if not c["ok"]]
        if errs or bad:
            raise StepError(f"{sec['section']}: portal errors {errs}, not kept {bad}")

    def read_back(self, f):
        if f["kind"] in ("radio", "checkbox"):
            got = "checked" if self.el(f["id"]).is_checked() else "unchecked"
            return {"label": f["label"], "want": "checked", "got": got, "ok": got == "checked"}
        if f["kind"] == "select":
            got = self.el(f["id"], ":combo_input").input_value()
        else:
            got = self.el(f["id"]).input_value()
        want = f["typed"]
        ok = norm(got) == norm(want)
        if not ok and f["kind"] == "number":
            try:
                ok = float(got) == float(want)
            except ValueError:
                pass
        return {"label": f.get("label") or f["id"], "want": want, "got": got, "ok": ok}


def run(steps, values, out, cdp=None, profile=None, close=False):
    from playwright.sync_api import sync_playwright
    report = {"procedure": steps["procedure"], "sections": [], "wizard_seconds": None}
    fields = {}
    for p in plan(steps, values):
        fields.setdefault(p["section"], []).append(p)
    out.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as pw:
        if cdp:
            browser = pw.chromium.connect_over_cdp(cdp)
            ctx = browser.contexts[0]
        else:
            ctx = pw.chromium.launch_persistent_context(
                str(profile), channel="chrome", headless=False, viewport={"width": 1280, "height": 900})
        page = ctx.new_page()
        runner = Runner(page, report)
        try:
            page.goto(steps["start"])
            if not page.url.startswith(steps["logged_in"]):
                print("Log in with SPID in the Chrome window; the run starts when you're in.", flush=True)
                page.wait_for_url(steps["logged_in"] + "**", timeout=15 * 60 * 1000)
            runner.settle()
            t0 = time.monotonic()
            for step in steps["wizard"]:
                runner.wizard(step, values)
                print(f"  {step['screen']}", flush=True)
            report["wizard_seconds"] = round(time.monotonic() - t0, 1)
            for sec in steps["sections"]:
                runner.section(sec, values, fields.get(sec["section"], []))
                print(f"  {sec['section']}: saved and read back", flush=True)
            report["total_seconds"] = round(time.monotonic() - t0, 1)
            report["result"] = "filled"
        except Exception as e:
            report["result"] = "stopped"
            report["error"] = f"{type(e).__name__}: {e}"
            if not page.is_closed():
                page.screenshot(path=str(out / "failure.png"), full_page=True)
        finally:
            report["url"] = None if page.is_closed() else page.url
            (out / "report.json").write_text(json.dumps(report, indent=2, ensure_ascii=False))
            print(json.dumps({k: report.get(k) for k in ("result", "error", "wizard_seconds", "total_seconds")}),
                  flush=True)
            if not close and not page.is_closed():
                print("Left open for you: tick the declarations, pay and sign yourself. Close the window when done.",
                      flush=True)
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
    ap.add_argument("--server", required=True, type=Path)
    ap.add_argument("--token", required=True)
    ap.add_argument("--out", type=Path)
    ap.add_argument("--cdp")
    ap.add_argument("--profile", type=Path, default=Path.home() / ".cache/pratica-portal/profile")
    ap.add_argument("--close", action="store_true")
    a = ap.parse_args()
    steps = yaml.safe_load(a.steps.read_text())
    try:
        values = Values(json.loads(a.server.read_text()), a.token, json.loads(a.answers.read_text()))
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
