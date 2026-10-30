#!/usr/bin/env python3
"""One page for the person to review a pratica Claude prepared: what's left for them, what
was checked, and every value as it went on the form or portal.

  review.py OUT [--route ROUTE.json]

OUT is the folder fill.py run or run.py run wrote (its report.json), or the one run.py page
wrote with the pane's `__pv.last()` saved there as report.json (its private/shown.json adds the
values and the person's steps; private/declarations.json the declarations' own text, and
private/anteprima.pdf the portal's PDF preview, which only `run.py run` copies); --route is
`fill.py route --json` for a form, for where it goes and the person's steps. Writes
OUT/review.html: one self-contained file (the form's pages inlined), to show the person
before they sign anything. It reads only those files.
"""

import argparse
import base64
import html
import json
import sys
from pathlib import Path

CSS = """
:root { --bg:#fff; --fg:#1d1d1f; --muted:#6e6e73; --line:#e5e5ea; --ok:#1a7f37; --bad:#c62828;
        --you:#fff8e1; --you-line:#f0c36d; --card:#f7f7f8; }
@media (prefers-color-scheme: dark) { :root { --bg:#161618; --fg:#f2f2f7; --muted:#a1a1a6;
        --line:#2c2c2e; --ok:#3fb950; --bad:#ff6b6b; --you:#2a2414; --you-line:#8a6d2b; --card:#1f1f22; } }
* { box-sizing: border-box; }
body { margin:0; background:var(--bg); color:var(--fg);
       font:15px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
main { max-width: 860px; margin: 0 auto; padding: 24px 16px 48px; }
h1 { font-size: 22px; margin: 0 0 4px; }
h2 { font-size: 16px; margin: 28px 0 8px; }
.sub { color: var(--muted); margin: 0 0 16px; }
.status { display:inline-block; font-weight:600; padding:2px 10px; border-radius:999px; border:1px solid; }
.status.ok { color: var(--ok); } .status.bad { color: var(--bad); }
.you { background: var(--you); border: 1px solid var(--you-line); border-radius: 10px; padding: 12px 16px; }
.you ol { margin: 0; padding-left: 20px; } .you li { margin: 4px 0; }
.facts { display:grid; grid-template-columns: max-content 1fr; gap: 4px 16px; margin: 0; }
.facts dt { color: var(--muted); } .facts dd { margin: 0; overflow-wrap: anywhere; }
ul.checks { list-style: none; padding: 0; margin: 0; }
ul.checks li::before { content: "✓ "; color: var(--ok); font-weight: 700; }
ul.checks li.bad::before { content: "✗ "; color: var(--bad); }
details { border: 1px solid var(--line); border-radius: 10px; margin: 8px 0; background: var(--card); }
summary { cursor: pointer; padding: 10px 14px; font-weight: 600; }
table { width: 100%; border-collapse: collapse; }
td { padding: 6px 14px; border-top: 1px solid var(--line); vertical-align: top; overflow-wrap: anywhere; }
td:first-child { color: var(--muted); width: 42%; }
.blank { color: var(--muted); font-style: italic; }
.decl { white-space: pre-wrap; padding: 6px 14px 10px; border-top: 1px solid var(--line); overflow-wrap: anywhere; }
.decl ul { list-style: none; margin: 8px 0 0; padding: 0; } .decl li.on { color: var(--bad); font-weight: 600; }
a.dl { display:inline-block; margin: 4px 0 8px; }
img.page { width: 100%; border: 1px solid var(--line); border-radius: 6px; margin: 8px 0; background: #fff; }
"""


def esc(x):
    return html.escape("" if x is None else str(x))


def page(title, subtitle, ok, status, yours, facts, checks, groups, extra="", top=""):
    out = [f"<!doctype html><html lang='it'><head><meta charset='utf-8'>"
           f"<meta name='viewport' content='width=device-width,initial-scale=1'>"
           f"<title>{esc(title)}</title><style>{CSS}</style></head><body><main>",
           f"<h1>{esc(title)}</h1><p class='sub'>{esc(subtitle)}</p>",
           f"<span class='status {'ok' if ok else 'bad'}'>{esc(status)}</span>"]
    if yours:
        out.append("<h2>What's left for you</h2><div class='you'><ol>"
                   + "".join(f"<li>{esc(y)}</li>" for y in yours) + "</ol></div>")
    out.append(top)
    if facts:
        out.append("<h2>Where it goes</h2><dl class='facts'>"
                   + "".join(f"<dt>{esc(k)}</dt><dd>{esc(v)}</dd>" for k, v in facts if v) + "</dl>")
    out.append("<h2>What was checked</h2><ul class='checks'>"
               + "".join(f"<li class='{'' if c_ok else 'bad'}'>{esc(c)}</li>" for c, c_ok in checks) + "</ul>")
    out.append("<h2>Every value</h2>")
    for name, rows in groups:
        out.append(f"<details open><summary>{esc(name)}</summary><table>"
                   + "".join(f"<tr><td>{esc(label)}</td><td{' class=blank' if blank else ''}>{esc(value)}</td></tr>"
                             for label, value, blank in rows) + "</table></details>")
    out.append(extra + "</main></body></html>")
    return "".join(out)


def text(v):
    if isinstance(v, list):
        return "; ".join(", ".join(f"{k}: {x}" for k, x in r.get("cells", r).items()) if isinstance(r, dict) else str(r)
                         for r in v)
    return v


def form_review(out, report, route):
    checks = report["checks"]
    ok = all(c["ok"] for c in checks)
    rows = [(r["label"], text(r.get("value")) if r["status"] in ("value", "answered") else
             f"left blank ({r.get('reason', 'nothing to write')})", r["status"] not in ("value", "answered"))
            for r in report["rows"]]
    yours, facts = [], []
    if route:
        # Every step but filling in the form, which the library words "Compilare ..." and
        # fill.py just did; a step for anyone (the planimetria, the F24) is still to do.
        yours = [s["do"] for s in route["steps"] if not s["do"].startswith("Compilare")]
        facts = [("Route", route["route"]), ("Send to", "; ".join(w.get("address", "") for w in route.get("where", []))),
                 ("Signing", "; ".join(s["text"] for s in route.get("signing", []))),
                 ("Attach", "; ".join(route.get("attach", []))), ("Deadline", route.get("deadline"))]
    pages = "".join(f"<img class='page' alt='{esc(p.name)}' src='data:image/png;base64,"
                    f"{base64.b64encode(p.read_bytes()).decode()}'>" for p in sorted(out.glob("page-*.png")))
    return page(route["title"] if route else report["entry"], f"{report['entry']} · {report['form']}", ok,
                "Ready for your review: every check passed" if ok else "Not ready: a check failed",
                yours, facts, [(c["check"] + (f": {c['detail']}" if c.get("detail") and not c["ok"] else ""), c["ok"])
                               for c in checks],
                [("Form fields", rows)], f"<h2>The filled form</h2>{pages}" if pages else "")


def decisions(report):
    """Choices the form took in the person's name (run.py DECIDED), apart from the long table of
    values: they state how an obligation will be met, so the person confirms each. html."""
    items = report.get("decisions") or []
    if not items:
        return ""
    out = ["<h2>Chosen in your name</h2><p class='sub'>The form was filled with these. Each states how you will "
           "meet an obligation: check that it is what you will do.</p><div class='you'><ul>"]
    for d in items:
        out.append(f"<li><b>{esc(d['section'])}</b><ul>" + "".join(f"<li>{esc(c)}</li>" for c in d["choices"])
                   + "</ul></li>")
    return "".join(out) + "</ul></div>"


def declarations(report, folder):
    """The declarations' own text, as the runner copied it from the portal (private/declarations.json):
    the page shows these, never Claude's summary of them. (html, checks)."""
    f = folder / "declarations.json" if folder else None
    items = json.loads(f.read_text()) if f and f.exists() else None
    note = report.get("declarations_note")
    checks = []
    if items:
        ticked = [i["section"] for i in items if any(b["ticked"] for b in i["boxes"])]
        checks.append((f"Declarations: {len(items)} copied word for word from the portal; none ticked" if not ticked
                       else "Declarations already ticked before you opened them (not by this run): "
                       + "; ".join(ticked), not ticked))
    elif items is not None and not note:
        checks.append(("Declarations: none in the portal's menu; read each one there before you tick it", False))
    elif items is None:
        checks.append(("Declarations: not copied in this run" + (" (read in the pane, but kept in its tab: they would "
                       "pass through the conversation)" if report.get("declarations_read") else "")
                       + "; read each one in the portal before you tick it", False))
    if note:
        checks.append((f"Declarations: not all copied ({note}); read the rest in the portal", False))
    if not items:
        return "", checks
    out = ["<h2>The declarations you will tick</h2><p class='sub'>The portal's own words, copied by the program, "
           "not a summary. Read them before you tick: you make them under your own name, and a false one is "
           "a crime (DPR 445/2000, art. 76).</p>"]
    for i in items:
        boxes = "".join(f"<li class='{'on' if b['ticked'] else ''}'>{'☑ already ticked' if b['ticked'] else '☐'} "
                        f"{esc(b['label'])}</li>" for b in i["boxes"])
        out.append(f"<details open><summary>{esc(i['section'])}</summary><div class='decl'>{esc(i['text'])}"
                   + (f"<ul>{boxes}</ul>" if boxes else "") + "</div></details>")
    return "".join(out), checks


def preview(report, folder):
    """The portal's PDF preview of the draft (private/anteprima.pdf): pages as images when PyMuPDF is
    there, always a download. (html, checks)."""
    f = folder / "anteprima.pdf" if folder else None
    if not (f and f.exists()):
        why = report.get("preview_note")
        return "", [("The portal's PDF preview " + (f"couldn't be copied ({why})" if why else "was not copied in this run")
                     + ": open Visualizza anteprima in the portal and read it before you sign", False)]
    data = f.read_bytes()
    out = ["<h2>The portal's preview</h2><p class='sub'>The document the portal will file, as it renders it.</p>",
           f"<a class='dl' download='anteprima.pdf' href='data:application/pdf;base64,"
           f"{base64.b64encode(data).decode()}'>Download the PDF</a>"]
    try:
        import fitz  # noqa: PLC0415  (optional, as in fill.py)
        with fitz.open(stream=data, filetype="pdf") as doc:
            for n, pg in enumerate(doc):
                if n == 20:
                    out.append(f"<p class='sub'>{len(doc) - 20} more pages in the PDF.</p>")
                    break
                png = pg.get_pixmap(matrix=fitz.Matrix(1.4, 1.4)).tobytes("png")
                out.append(f"<img class='page' alt='page {n + 1}' src='data:image/png;base64,"
                           f"{base64.b64encode(png).decode()}'>")
    except Exception:
        out.append("<p class='sub'>(Open the PDF to read it: this page can't draw it without PyMuPDF.)</p>")
    return "".join(out), [("The portal's own PDF preview copied", True)]


def portal_review(report, out=None):
    ok = report.get("result") == "filled"
    checks = [(f"{s['section']}: saved, every field read back ({s['seconds']} s)"
               if not s["errors"] and not s["bad"] else f"{s['section']}: {s['errors'] or s['bad']}",
               not s["errors"] and not s["bad"]) for s in report.get("sections", [])]
    if report.get("wizard_seconds") is not None:
        checks.insert(0, (f"Wizard: every screen confirmed ({report['wizard_seconds']} s)", True))
    if report.get("verify_seconds") is not None:
        checks.append((f"Draft loaded again from the portal: every value still there ({report['verify_seconds']} s)"
                       if ok else "Draft loaded again from the portal: a value didn't stay (see Stopped)", ok))
    if report.get("error"):
        checks.append((f"Stopped: {report['error']}", False))
    # The report says only why a read-back failed; the person sees what it wanted and found.
    for d in report.get("details", []):
        checks.append((f"{d['section']}, {d['label']}: wanted {d['want']!r}, the portal had {d['got']!r}", False))
    groups = {}
    for v in report.get("values", []):
        groups.setdefault(v["section"], []).append((v["label"], v["value"], False))
    folder = out / "private" if out else None
    # A stopped run hasn't reached either, so it says nothing about them.
    decl_html, decl_checks = declarations(report, folder) if ok else ("", [])
    prev_html, prev_checks = preview(report, folder) if ok else ("", [])
    checks += decl_checks + prev_checks
    return page(report.get("title") or report["procedure"], f"{report['procedure']} · draft on the portal", ok,
                f"Draft saved in {report.get('total_seconds')} s: ready for your review" if ok
                else "Stopped before the end: see below", report.get("yours", []), [("Draft", report.get("url"))],
                checks, list(groups.items()), prev_html, decisions(report) + decl_html)


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("out", type=Path)
    ap.add_argument("--route", type=Path)
    a = ap.parse_args()
    report = json.loads((a.out / "report.json").read_text())
    shown = a.out / "private" / "shown.json"  # run.py page: the pane's report has only the run
    if shown.exists():
        report |= json.loads(shown.read_text())
    route = json.loads(a.route.read_text()) if a.route else None
    doc = form_review(a.out, report, route) if "rows" in report else portal_review(report, a.out)
    (a.out / "review.html").write_text(doc, encoding="utf-8")
    print(a.out / "review.html")
    return 0


if __name__ == "__main__":
    sys.exit(main())
