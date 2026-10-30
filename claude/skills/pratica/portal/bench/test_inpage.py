#!/usr/bin/env python3
"""Offline tests of inpage.js on a fake portal: no network, no vault, no model.

  ~/.cache/pratica-portal/venv/bin/python -I claude/skills/pratica/portal/bench/test_inpage.py

A local server plays the portal (portal.html, an attachments screen): it stores what Salva
and the upload boxes send and shows it again on every load, so the runner's fresh load of
the draft reads what the "portal" kept. Each case runs run.py's page scripts in headless
Chromium, as the browser pane runs them, and checks the report and the bytes that arrived.
"""

import base64
import contextlib
import hashlib
import io
import json
import os
import shutil
import sys
import tempfile
import threading
from email.parser import BytesParser
from email.policy import default
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
sys.dont_write_bytecode = True  # no __pycache__ in the skill's folder
import run  # noqa: E402


DEN, IMPRESA = "form:r0c1:dynaForm:den_txt", "form:r4c1:dynaForm:impresa_txt"


class Portal:
    """What the fake portal stored. drop: it shows Denominazione after Salva but never stores it.
    slow: the next that many loads of ?flaky=1 come without the second section's box, as
    the real portal sometimes shows a section before its boxes."""

    def reset(self, drop=False, slow=0):
        self.fields, self.files, self.drop, self.sent, self.slow = {}, [], drop, False, slow


PORTAL = Portal()
# Files the bench serves by link (/files/<name>), as an https host would for the real portal.
LINKED = {}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def send(self, body, ctype="application/json"):
        self.send_response(200)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = self.path.split("?")[0]
        if path == "/":
            self.send(b"<!doctype html><title>bench</title>", "text/html")
        elif path == "/portal.html":
            html = (HERE / "portal.html").read_bytes()
            if "flaky=1" in self.path and PORTAL.slow > 0:
                PORTAL.slow -= 1
                html = html.replace(b'<tr><td><label class="labelDynaFormControl" id="impresa-label">Impresa esecutrice</label></td>\n'
                                    b'        <td><input type="text" id="form:r4c1:dynaForm:impresa_txt"></td></tr>', b"")
                assert b"impresa_txt" not in html, "the bench page changed: update the flaky cut"
            self.send(html, "text/html; charset=utf-8")
        elif path == "/anteprima.pdf":
            self.send(b"%PDF-1.4\nl'anteprima del portale finto\n", "application/pdf")
        elif path.startswith("/files/") and path[7:] in LINKED:
            self.send(LINKED[path[7:]], "application/pdf")
        elif path == "/state":
            self.send(json.dumps({"fields": PORTAL.fields, "files": [f["name"] for f in PORTAL.files],
                                  "boxes": {f["box"]: f["name"] for f in PORTAL.files}}).encode())
        else:
            self.send_error(404)

    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        if self.path == "/save":
            PORTAL.fields = json.loads(body)["fields"]
            if PORTAL.drop:
                PORTAL.fields.pop(DEN, None)
        elif self.path == "/upload":
            msg = BytesParser(policy=default).parsebytes(
                b"Content-Type: " + self.headers["Content-Type"].encode() + b"\r\n\r\n" + body)
            for part in msg.iter_parts():
                data = part.get_payload(decode=True) or b""
                PORTAL.files.append({"name": part.get_filename(), "type": part.get_content_type(),
                                     "sha256": hashlib.sha256(data).hexdigest(),
                                     "box": part.get_param("name", header="content-disposition")})
        elif self.path == "/sent":
            PORTAL.sent = True
        self.send(b"{}")


def steps(base, start="/portal.html", **over):
    sec = {"section": "Allega documenti", "open": False, "fields": [
        {"id": "den_txt", "kind": "text", "label": "Denominazione", "value": "{{ask.den}}"},
        {"id": "planimetria_input", "kind": "file", "label": "Planimetria quotata", "value": "{{ask.planimetria}}"},
        {"id": "ricevuta_input", "kind": "file", "label": "Ricevuta ASL", "value": "{{ask.ricevuta}}"},
    ]} | over
    tecnici = {"section": "Dati tecnici", "fields": [
        {"id": "impresa_txt", "kind": "text", "label": "Impresa esecutrice", "value": "{{ask.impresa}}"}]}
    return {"procedure": "test/allegati", "start": base + start, "logged_in": base + "/", "wizard": [],
            "sections": [sec, tecnici]}


def fill(browser, base, st, answers, files_limit=None, resume=None, urls=None, sizes=None, held=None):
    values = run.Values(answers, fields={"x:ruolo.tipo": "titolare"})
    install, run_js = run.page_scripts(st, values, files_limit, resume=resume, urls=urls)
    if sizes is not None:
        sizes.append(len(run_js))
    page = browser.new_page()
    try:
        page.goto(base + "/")
        page.evaluate(install)
        page.evaluate(run_js)
        while (report := page.evaluate("__pv.wait(5000)"))["result"] == "running":
            pass
        if held is not None:  # what the report leaves out, as run.py reads it for the person
            held.append(page.evaluate("__pv.held()"))
        return report
    finally:
        page.close()


def type_code(page, code):
    """The person types the code into the box the runner opened."""
    page.wait_for_selector('[data-pv="code"] input')
    page.fill('[data-pv="code"] input', code)


def settle(page):
    """Polls as the skill does, until the run is neither running nor waiting for the code."""
    while (report := page.evaluate("__pv.wait(5000)"))["result"] in ("running", "waiting"):
        pass
    return report


def main():
    from playwright.sync_api import sync_playwright
    tmp = Path(tempfile.mkdtemp(prefix="inpage-bench-"))
    planimetria, ricevuta = tmp / "planimetria.pdf", tmp / "ricevuta-asl.pdf.p7m"
    planimetria.write_bytes(b"%PDF-1.4\n" + os.urandom(3 * 1024 * 1024))
    ricevuta.write_bytes(b"finta ricevuta firmata\n")
    sha = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in (planimetria, ricevuta)}
    answers = {"den": "PIZZERIA FITTIZIA DI GENNARO FITTIZIO", "planimetria": str(planimetria),
               "ricevuta": str(ricevuta), "impresa": "EDIL FITTIZIA DI MARCO ESEMPIO"}
    # What the pane can carry (256 KB of files at most), for the sealed runs that stop and resume:
    # the tab keeps the sealed values in sessionStorage, which a 3 MB drawing would overflow.
    tiny = tmp / "tiny.pdf"
    tiny.write_bytes(b"%PDF-1.4 tiny\n")
    small = answers | {"planimetria": str(tiny), "ricevuta": str(tiny)}
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{server.server_port}"
    failed, ran = [], []

    def case(name, check):
        ran.append(name)
        try:
            check()
            print(f"ok    {name}")
        except AssertionError as e:
            failed.append(name)
            print(f"FAIL  {name}: {e}")

    def stops(report, words):
        assert report["result"] == "stopped", f"result {report['result']}"
        assert words in report.get("error", ""), f"error {report.get('error')!r}"
        assert not PORTAL.sent, "Firma e invio was clicked"

    with sync_playwright() as pw:
        browser = pw.chromium.launch()

        def kept():
            PORTAL.reset()
            r = fill(browser, base, steps(base), answers)
            assert r["result"] == "filled", r.get("error")
            assert r.get("verify_seconds") is not None, "no fresh load of the draft"
            assert PORTAL.fields.get(DEN) == answers["den"], f"stored {PORTAL.fields}"
            assert PORTAL.fields.get(IMPRESA) == answers["impresa"], f"stored {PORTAL.fields}"
            got = {f["name"]: f["sha256"] for f in PORTAL.files}
            assert got == sha, f"received {got}"
            assert {f["type"] for f in PORTAL.files} == {"application/pdf", "application/pkcs7-mime"}
            assert not PORTAL.sent, "Firma e invio was clicked"
        case("fills, attaches 3 MB byte-exact, and the fresh load finds it all", kept)

        def dropped():
            PORTAL.reset(drop=True)
            stops(fill(browser, base, steps(base), answers), "fresh load of the draft: not kept")
        case("a value the portal shows but never stores stops the run", dropped)

        def wrong_label():
            PORTAL.reset()
            st = steps(base)
            st["sections"][0]["fields"][2]["label"] = "Visura camerale"
            stops(fill(browser, base, st, answers), "expected the label Visura camerale")
        case("a file box with another label stops the run", wrong_label)

        def banner():
            PORTAL.reset()
            stops(fill(browser, base, steps(base, start="/portal.html?banner=1"), answers), "covered by div#consenso")
        case("a consent banner over the form stops the run", banner)

        def fading_list():
            PORTAL.reset()
            r = fill(browser, base, steps(base, start="/portal.html?fade=1"), answers)
            assert r["result"] == "filled", r
        case("a suggestion list still fading out over the next box is closed, not a stop", fading_list)

        def signed_slot():
            # The portal's "required" under a slot that takes only signed files is the person's;
            # under a plain one it stops the run, and the snapshot names the slot by its title.
            PORTAL.reset()
            r = fill(browser, base, steps(base, start="/portal.html?slot=signed"), answers)
            assert r["result"] == "filled", r
            PORTAL.reset()
            r = fill(browser, base, steps(base, start="/portal.html?slot=plain"), answers)
            assert r["result"] == "stopped" and "obbligatorio" in r["error"], r
            box = next(b for b in r["snapshot"]["boxes"] if b["id"] == "procura_input")
            assert box["label"] == "procura speciale" and box["required"] and box["accepts"] == "pdf;pdf.p7m", box
            assert ".pdf" not in json.dumps(r["snapshot"]), "a file's name in the snapshot"
        case("a required slot taking only signed files is the person's; a plain one stops, named by its title", signed_slot)

        def theirs():
            PORTAL.reset()
            stops(fill(browser, base, steps(base, save="Firma e invio"), answers), "refused to click")
        case("the person's Firma e invio is never clicked", theirs)

        decl_start = "/portal.html?decl=1"

        def declarations_read():
            PORTAL.reset()
            held = []
            r = fill(browser, base, steps(base, start=decl_start), answers, held=held)
            assert r["result"] == "filled", r.get("error")
            assert r["declarations_read"] == 1 and "declarations_note" not in r, r
            d = held[0]["declarations"]
            assert len(d) == 1 and "consapevolezza" in d[0]["section"], d
            assert "dichiara quanto segue" in d[0]["text"] and "art. 76" in d[0]["text"], d[0]["text"]
            assert d[0]["boxes"] == [{"label": "Per presa visione", "kind": "checkbox", "ticked": False},
                                     {"label": "che i locali possiedono i requisiti di sorvegliabilità", "kind": "radio",
                                      "ticked": False}], d[0]["boxes"]  # one radio, not its clone, with its own label
            assert "SALVA" not in d[0]["text"], d[0]["text"]
            assert "sanzioni penali" not in json.dumps(r), "the report carries the declaration's own text"
            assert not PORTAL.sent, "Firma e invio was clicked"
        case("the declarations' own text is copied into held(), ticked by nothing, and not into the report", declarations_read)

        def declarations_already_ticked():
            PORTAL.reset()
            held = []
            r = fill(browser, base, steps(base, start=decl_start + "&ticked=1"), answers, held=held)
            assert r["result"] == "filled", r.get("error")
            assert held[0]["declarations"][0]["boxes"][0]["ticked"] is True, held[0]["declarations"]
        case("a declaration the portal already shows ticked is reported as ticked", declarations_already_ticked)

        def declarations_stuck():
            PORTAL.reset()
            held = []
            r = fill(browser, base, steps(base, start=decl_start + "&stuck=1"), answers, held=held)
            assert r["result"] == "filled", f"a declaration that won't open stopped the run: {r.get('error')}"
            assert r["declarations_read"] == 0 and "didn't open" in r["declarations_note"], r
            assert held[0]["declarations"] == []
        case("a declarations section that won't open is a note, not a stop", declarations_stuck)

        def no_declarations():
            PORTAL.reset()
            held = []
            r = fill(browser, base, steps(base), answers, held=held)
            assert r["result"] == "filled" and r["declarations_read"] == 0 and held[0]["declarations"] == [], r
        case("a portal with no declarations in its menu reads none", no_declarations)

        def preview_copied():
            PORTAL.reset()
            values = run.Values(answers, fields={"x:ruolo.tipo": "titolare"})
            st = steps(base, start=decl_start) | {"preview_url": "/anteprima.pdf"}
            install, run_js = run.page_scripts(st, values, None)
            folder = tmp / "preview"
            folder.mkdir(exist_ok=True)
            (folder / "anteprima.pdf").unlink(missing_ok=True)
            page = browser.new_page()
            try:
                page.goto(base + "/")
                page.evaluate(install)
                page.evaluate(run_js)
                while (report := page.evaluate("__pv.wait(5000)"))["result"] == "running":
                    pass
                assert report["result"] == "filled", report.get("error")
                assert report["preview_pdf"] is True and "preview_note" not in report, report
                ok, why = run.capture_preview(page, folder)
                assert ok, why
                assert (folder / "anteprima.pdf").read_bytes().startswith(b"%PDF-1.4\n"), "not the portal's PDF"
                assert (folder / "anteprima.pdf").stat().st_mode & 0o777 == 0o600
                assert page.query_selector('[data-pv="review"] iframe[src^="blob:"]'), "the preview isn't on the review"
                assert page.url.startswith(base + "/"), "the preview moved the page"
            finally:
                page.close()
        case("the portal's PDF is fetched with the person's session, kept private and shown on the review, with no navigation", preview_copied)

        def review_drawn():
            PORTAL.reset()
            values = run.Values(answers, fields={"x:ruolo.tipo": "titolare"})
            install, run_js = run.page_scripts(steps(base, start=decl_start), values, None)
            page = browser.new_page()
            try:
                page.goto(base + "/")
                page.evaluate(install)
                page.evaluate(run_js)
                while (report := page.evaluate("__pv.wait(5000)"))["result"] == "running":
                    pass
                assert report["result"] == "filled", report.get("error")
                inside = "document.querySelector('[data-pv=review]').shadowRoot"  # drawn in a shadow root
                text = page.evaluate(f"{inside}.firstElementChild.nextElementSibling.innerText")
                assert "Draft saved on the portal" in text and "nothing has been declared".lower() in text.lower(), text[:200]
                assert "dichiara quanto segue" in text and "Per presa visione" in text, "the declaration isn't on the review"
                assert "che i locali possiedono i requisiti di sorvegliabilità" in text, "the radio's own label is missing"
                everything = page.evaluate(f"{inside}.textContent")  # the values sit in closed <details>
                assert answers["den"] in everything and answers["impresa"] in everything, "the values typed aren't on the review"
                assert "dichiara quanto segue" not in json.dumps(report), "the report carries the declaration"
                page.click("text=Show the draft")
                assert page.query_selector('[data-pv="review"]') is None, "Show the draft left the review up"
                page.evaluate("__pv.review()")
                assert page.query_selector('[data-pv="review"]'), "__pv.review() didn't draw it again"
            finally:
                page.close()
        case("a filled run draws the declarations and every value over the portal, none of it in the report", review_drawn)

        def preview_missing():
            PORTAL.reset()
            values = run.Values(answers, fields={"x:ruolo.tipo": "titolare"})
            install, run_js = run.page_scripts(steps(base), values, None)
            folder = tmp / "no-preview"
            folder.mkdir(exist_ok=True)
            page = browser.new_page()
            try:
                page.goto(base + "/")
                page.evaluate(install)
                page.evaluate(run_js)
                while (report := page.evaluate("__pv.wait(5000)"))["result"] == "running":
                    pass
                assert report["result"] == "filled", f"a missing preview stopped the run: {report.get('error')}"
                assert report["preview_pdf"] is False and "404" in report["preview_note"], report
                ok, why = run.capture_preview(page, folder)
                assert not ok and "no preview" in why, (ok, why)
                assert not (folder / "anteprima.pdf").exists()
                assert "Not copied" in page.evaluate("document.querySelector('[data-pv=review]').shadowRoot.textContent"), \
                    "the review doesn't say it has no preview"
            finally:
                page.close()
        case("a portal that gives no preview is a note on the review, not a stop", preview_missing)


        def recovered():
            # The portal renamed a label: the run stops there and says so, keeping what the
            # first section typed; a fix for that one field resumes it on the same draft,
            # without redoing the first section, and the fresh load checks both.
            PORTAL.reset()
            st = steps(base, start="/portal.html?rename=1")
            h = []
            first = fill(browser, base, st, answers, held=h)
            assert first["result"] == "stopped" and "expected the label Impresa esecutrice" in first["error"], first
            at = first["at"]
            assert (at["phase"], at["index"], at["field"]) == ("section", 1, 0), at
            assert "kept" not in first, "the report carries what the sections typed"
            assert [k["section"] for k in h[0]["kept"]] == [0], h[0]["kept"]
            first = first | {"kept": h[0]["kept"]}  # run.py joins private/kept.json to report.json
            again = fill(browser, base, st, answers, resume=run.resumption(first))
            assert again["result"] == "stopped" and again["at"]["index"] == 1, "resumed without the fix and got past"
            fix = [{"section": 1, "field": 0, "set": {"label": "Ditta esecutrice"}}]
            done = fill(browser, base, st, answers, resume=run.resumption(first, fix))
            assert done["result"] == "filled", done.get("error")
            assert done["verify_seconds"] is not None and done["resumed"], done
            assert [s["section"] for s in done["sections"]] == ["Dati tecnici"], "redid a finished section"
            assert PORTAL.fields.get(DEN) == answers["den"] and PORTAL.fields.get(IMPRESA) == answers["impresa"], \
                PORTAL.fields
            assert len(PORTAL.files) == 2, f"uploaded {len(PORTAL.files)} files, not 2"
            assert not PORTAL.sent, "Firma e invio was clicked"
            out = tmp / "drift"
            out.mkdir()
            run.drift(out, st, run.resumption(first, fix))
            assert json.loads((out / "drift.json").read_text())["fixes"] == fix
        case("a renamed label stops in place; a fix resumes the same draft, checked end to end", recovered)

        def lost_before():
            # A resume whose fresh load finds an earlier section's value gone still stops.
            PORTAL.reset()
            st = steps(base, start="/portal.html?rename=1")
            h = []
            first = fill(browser, base, st, answers, held=h) | {}
            first["kept"] = h[0]["kept"]
            PORTAL.fields.pop(DEN, None)
            fix = [{"section": 1, "field": 0, "set": {"label": "Ditta esecutrice"}}]
            done = fill(browser, base, st, answers, resume=run.resumption(first, fix))
            stops(done, "fresh load of the draft: not kept")
        case("after a resume, a value lost from an earlier section is still caught", lost_before)

        def checked():
            # check.js names what the pane's storage lacks, byte for byte, and nothing once
            # install.js has run; a changed steps file makes only steps.js stale again.
            PORTAL.reset()
            values = run.Values(answers, fields={"x:ruolo.tipo": "titolare"})
            files = run.page_files(steps(base), values, files_limit=None)
            page = browser.new_page()
            try:
                page.goto(base + "/")
                page.evaluate("localStorage.clear()")
                assert page.evaluate(files["check.js"]) == "run runner.js, then steps.js, then run.js"
                page.evaluate(files["install.js"])
                assert page.evaluate(files["check.js"]) == "run run.js"
                other = steps(base)
                other["sections"][1]["fields"][0]["label"] = "Ditta esecutrice"
                again = run.page_files(other, values, files_limit=None)
                assert page.evaluate(again["check.js"]) == "run steps.js, then run.js"
                elsewhere = run.page_files(steps(base) | {"logged_in": "https://example.invalid/"}, values, files_limit=None)
                assert page.evaluate(elsewhere["check.js"]).startswith("go to https://example.invalid"), "wrong origin passed"
            finally:
                page.close()
        case("check.js says which of runner.js and steps.js the pane lacks", checked)

        def resumed_in_tab():
            # A stopped run's report and values stay in the tab: a reloaded runner resumes it
            # on the same draft with one fix, and one frame lies over the page, not two.
            PORTAL.reset()
            st = steps(base, start="/portal.html?rename=1")
            values = run.Values(answers, fields={"x:ruolo.tipo": "titolare"})
            install, run_js = run.page_scripts(st, values, files_limit=None)
            page = browser.new_page()
            try:
                page.goto(base + "/")
                page.evaluate(install)
                page.evaluate(run_js)
                while (first := page.evaluate("__pv.wait(5000)"))["result"] == "running":
                    pass
                assert first["result"] == "stopped", first
                page.evaluate("delete window.__pv; (0, eval)(localStorage.getItem('pv-runner'))")
                assert page.evaluate("__pv.last().result") == "stopped", "the report didn't outlive the runner"
                assert "kept" not in page.evaluate("__pv.last()"), "the stored report carries what the sections typed"
                page.evaluate('__pv.resume({fixes: [{section: 1, field: 0, set: {label: "Ditta esecutrice"}}]})')
                while (done := page.evaluate("__pv.wait(5000)"))["result"] == "running":
                    pass
                assert done["result"] == "filled" and done["resumed"], done.get("error")
                assert [x["section"] for x in done["sections"]] == ["Dati tecnici"], "redid a finished section"
                assert page.evaluate("document.querySelectorAll('iframe').length") == 1, "frames piled up"
                assert PORTAL.fields.get(IMPRESA) == answers["impresa"], PORTAL.fields
            finally:
                page.close()
        case("a stopped run resumes in the tab after the runner reloads, one frame at a time", resumed_in_tab)

        def hiccup():
            # The section shows without its box on the first load: the run picks itself up on
            # the same draft, without redoing the first section or its uploads.
            PORTAL.reset(slow=1)
            r = fill(browser, base, steps(base, start="/portal.html?flaky=1"), answers, resume={"retry_wait_ms": 100})
            assert r["result"] == "filled", r.get("error")
            assert len(r.get("retries", [])) == 1 and "fields with this label" in r["retries"][0]["error"], r.get("retries")
            assert r["verify_seconds"] is not None, "no fresh load of the draft"
            assert PORTAL.fields.get(IMPRESA) == answers["impresa"] and PORTAL.fields.get(DEN) == answers["den"], PORTAL.fields
            assert len(PORTAL.files) == 2, f"uploaded {len(PORTAL.files)} files, not 2"
            assert answers["impresa"] not in json.dumps(r), "a value in the report"
        case("a portal hiccup (a box not shown yet) resumes the same draft by itself, once", hiccup)

        def stubborn():
            # A box that never shows is retried twice, then the run stops and says so; a renamed
            # label (a real change) is never retried.
            PORTAL.reset(slow=99)
            r = fill(browser, base, steps(base, start="/portal.html?flaky=1"), answers, resume={"retry_wait_ms": 100})
            stops(r, "fields with this label")
            assert len(r.get("retries", [])) == 2, r.get("retries")
            PORTAL.reset()
            r = fill(browser, base, steps(base, start="/portal.html?rename=1"), answers, resume={"retry_wait_ms": 100})
            stops(r, "expected the label Impresa esecutrice")
            assert not r.get("retries"), r.get("retries")
        case("retries are bounded, and a renamed label is never retried", stubborn)

        def silent():
            # A run that stops on a lost value tells a caller which box and why, never what it
            # held: the report, its snapshot and the stored copy have no value in them, while
            # held() (for the person's review page) has what was wanted.
            PORTAL.reset(drop=True)
            h = []
            r = fill(browser, base, steps(base), answers, held=h)
            assert r["result"] == "stopped" and "fresh load of the draft: not kept" in r["error"], r
            blob = json.dumps(r, ensure_ascii=False).lower()
            for v in (answers["den"], answers["impresa"]):
                assert v.lower() not in blob, f"the report carries {v!r}: {blob[:300]}"
            assert '"want"' not in blob and '"got"' not in blob and '"kept"' not in blob, blob[:300]
            assert '"why":"empty"' in r["error"] or '"why":"missing"' in r["error"], r["error"]
            snap = r["snapshot"]
            assert snap and snap["boxes"], snap
            assert any(b["state"] == "filled" for b in snap["boxes"]), "look() shows no filled box"
            assert all(set(b) <= {"id", "label", "kind", "state", "options"} for b in snap["boxes"]), snap["boxes"][0]
            assert any(d["want"] == answers["den"] for d in h[0]["details"]), "held() lost what was wanted"
        case("a stopped run's report, snapshot and stored copy hold no value; held() has them", silent)

        def echoed():
            # A portal message that repeats a value is scrubbed from the error.
            PORTAL.reset()
            r = fill(browser, base, steps(base, start="/portal.html?rename=1"), answers | {"impresa": "Ditta esecutrice"})
            assert "expected the label Impresa esecutrice" in r["error"], r["error"]
            assert "ditta esecutrice" not in r["error"].lower() and "‹value›" in r["error"], r["error"]
        case("a portal label that repeats a value is scrubbed from the error", echoed)

        def sealed_page(st, code):
            values = run.Values(small, fields={"x:ruolo.tipo": "titolare"})
            install, run_js = run.page_scripts(st, values, files_limit=None, code=code)
            page = browser.new_page()
            page.goto(base + "/")
            page.evaluate(install)
            return page, run_js

        def clean_of_values(page, code):
            dump = page.evaluate("JSON.stringify([localStorage, sessionStorage])")
            for secret in (answers["den"], answers["impresa"], code):
                assert secret not in dump, f"the tab's storage holds {secret!r}"

        def sealed():
            # run.js carries the values sealed; the person's code opens them in the page, a wrong
            # one is refused and asked again, and no storage ever holds them or the code.
            PORTAL.reset()
            code = run.new_code()
            page, run_js = sealed_page(steps(base), code)
            try:
                for secret in (answers["den"], answers["impresa"], base64.b64encode(b"%PDF-1.4 tiny\n").decode()):
                    assert secret not in run_js, f"run.js carries {secret!r}"
                assert page.evaluate(run_js) == "waiting for the code"
                first = page.evaluate("__pv.wait(300)")
                assert first["result"] == "waiting" and first["waiting"], first
                assert page.evaluate("document.querySelector('[data-pv=code] input').type") == "password"
                type_code(page, run.pretty_code(run.new_code()))
                page.wait_for_function("((document.querySelector('[data-pv=code]') || {}).textContent || '').includes(\"didn't open\")")
                type_code(page, run.pretty_code(code).lower())
                done = settle(page)
                assert done["result"] == "filled", done.get("error")
                assert PORTAL.fields.get(DEN) == answers["den"] and PORTAL.fields.get(IMPRESA) == answers["impresa"], PORTAL.fields
                assert len(PORTAL.files) == 2 and {f["sha256"] for f in PORTAL.files} == {hashlib.sha256(tiny.read_bytes()).hexdigest()}, PORTAL.files
                assert page.query_selector("[data-pv=code]") is None, "the code box stayed"
                clean_of_values(page, code)
            finally:
                page.close()
        case("a sealed run.js: the person's code opens it in the page, a wrong one is refused, nothing is stored in the clear", sealed)

        def never_opens():
            PORTAL.reset()
            page, run_js = sealed_page(steps(base), run.new_code())
            try:
                page.evaluate(run_js)
                for _ in range(5):
                    type_code(page, run.pretty_code(run.new_code()))
                done = settle(page)
                assert done["result"] == "stopped" and "the code didn't open the values" in done["error"], done
                assert not PORTAL.fields, "a run started on a wrong code"
            finally:
                page.close()
        case("five wrong codes end the sealed run before anything is filled", never_opens)

        def sealed_stopped(reload):
            PORTAL.reset()
            code = run.new_code()
            page, run_js = sealed_page(steps(base, start="/portal.html?rename=1"), code)
            try:
                page.evaluate(run_js)
                type_code(page, code)
                first = settle(page)
                assert first["result"] == "stopped" and "expected the label Impresa esecutrice" in first["error"], first
                clean_of_values(page, code)
                held = json.loads(page.evaluate("sessionStorage.getItem('pv-held')"))
                assert {"n", "c", "t"} <= set(held) and "kept" not in held, "what the first section typed isn't sealed"
                if reload:
                    page.evaluate("delete window.__pv; (0, eval)(localStorage.getItem('pv-runner'))")
                fix = '__pv.resume({fixes: [{section: 1, field: 0, set: {label: "Ditta esecutrice"}}]})'
                assert page.evaluate(fix) == ("waiting for the code" if reload else "started")
                if reload:
                    type_code(page, code)
                else:
                    assert page.query_selector("[data-pv=code]") is None, "asked for the code again in the same runner"
                done = settle(page)
                assert done["result"] == "filled" and done["resumed"], done.get("error")
                assert [s["section"] for s in done["sections"]] == ["Dati tecnici"], "redid a finished section"
                clean_of_values(page, code)
            finally:
                page.close()
        case("a sealed run that stopped resumes in the same runner with no new code", lambda: sealed_stopped(False))
        case("after the runner reloads, a sealed run resumes once the code is typed again", lambda: sealed_stopped(True))

        browser.close()

    def pane_limit():
        values = run.Values(answers, fields={"x:ruolo.tipo": "titolare"})
        try:
            run.page_scripts(steps(base), values)
        except run.StepError as e:
            assert "256 KB" in str(e), str(e)
        else:
            raise AssertionError("3 MB of files went into run.js for the pane")
    case("the pane's run.js refuses more than 256 KB of files", pane_limit)

    with sync_playwright() as pw:
        browser = pw.chromium.launch()

        def by_link():
            PORTAL.reset()
            LINKED["planimetria.pdf"] = planimetria.read_bytes()
            sizes = []
            r = fill(browser, base, steps(base), answers, files_limit=run.PAGE_FILES_LIMIT, sizes=sizes,
                     urls={str(planimetria): base + "/files/planimetria.pdf"})
            assert r["result"] == "filled", r.get("error")
            assert {f["name"]: f["sha256"] for f in PORTAL.files} == sha, PORTAL.files
            assert sizes[0] < 10_000, f"run.js is {sizes[0]} bytes"
        case("a big file by link: fetched in the page, checked, attached byte-exact; run.js stays small", by_link)

        def other_bytes():
            PORTAL.reset()
            LINKED["planimetria.pdf"] = b"%PDF-1.4 not the person's file"
            r = fill(browser, base, steps(base), answers, files_limit=run.PAGE_FILES_LIMIT,
                     urls={str(planimetria): base + "/files/planimetria.pdf"})
            stops(r, "the link served other bytes")
            assert not PORTAL.files or all(f["name"] != "planimetria.pdf" for f in PORTAL.files), PORTAL.files
        case("a link that serves other bytes stops the run before attaching", other_bytes)

        browser.close()

    def not_https():
        values = run.Values(answers, fields={"x:ruolo.tipo": "titolare"})
        try:
            run.page_scripts(steps(base), values, urls={str(planimetria): "http://example.com/p.pdf"})
        except run.StepError as e:
            assert "isn't https" in str(e), str(e)
        else:
            raise AssertionError("a plain-http link was accepted")
    case("a file link must be https", not_https)

    def sealing():
        code = run.new_code()
        assert len(code) == 15 and set(code) <= set(run.SEAL_ALPHABET), code
        plain = "Mario Rossi RSSMRA80A01F839Y".encode()
        s = run.seal(plain, code)
        assert run.unseal(s, code) == plain and plain not in base64.b64decode(s["c"])
        for other in (run.new_code(), code.lower()):
            try:
                run.unseal(s, other)
            except run.StepError:
                pass
            else:
                raise AssertionError("a wrong code opened it")
        ct = bytearray(base64.b64decode(s["c"]))
        ct[0] ^= 1
        try:
            run.unseal(s | {"c": base64.b64encode(bytes(ct)).decode()}, code)
        except run.StepError:
            pass
        else:
            raise AssertionError("a changed ciphertext opened")
    case("seal: round trip, a wrong code and a changed byte are refused", sealing)

    def cli_page():
        # `page` seals by default and shows the code only to the window; --plain leaves the values
        # in the clear and shows nothing; with no window to show it in, nothing is left to run.
        (tmp / "s.yaml").write_text(run.yaml.safe_dump(steps(base), allow_unicode=True))
        (tmp / "a.json").write_text(json.dumps(small))
        (tmp / "v.json").write_text(json.dumps({"x:ruolo.tipo": "titolare"}))
        shown, real = [], run.show_code

        def page_cmd(out, *extra):
            argv, buf = sys.argv, io.StringIO()
            sys.argv = ["run.py", "page", "--steps", str(tmp / "s.yaml"), "--answers", str(tmp / "a.json"),
                        "--values", str(tmp / "v.json"), "--out", str(out), *extra]
            try:
                with contextlib.redirect_stdout(buf), contextlib.redirect_stderr(buf):
                    return run.main(), buf.getvalue()
            finally:
                sys.argv = argv
        run.show_code = lambda code, give_up=None: shown.append(code)
        try:
            rc, text = page_cmd(tmp / "cli-sealed")
            js = (tmp / "cli-sealed" / "run.js").read_text()
            assert rc == 0 and len(shown) == 1, (rc, text)
            assert shown[0] not in text and run.pretty_code(shown[0]) not in text and shown[0] not in js
            assert small["den"] not in js and "startSealed" in js, js[:200]
            rc, text = page_cmd(tmp / "cli-plain", "--plain")
            assert rc == 0 and len(shown) == 1, "--plain showed a code"
            assert small["den"] in (tmp / "cli-plain" / "run.js").read_text()

            def no_window(code, give_up=None):
                raise run.StepError("no window")
            run.show_code = no_window
            rc, text = page_cmd(tmp / "cli-nowindow")
            assert rc == 1 and not (tmp / "cli-nowindow" / "run.js").exists(), "a sealed run.js was left with no code shown"
        finally:
            run.show_code = real
    case("run.py page seals by default, shows the code only to the window, and --plain says so", cli_page)

    def no_record():
        try:
            run.resumption({"url": "https://example.invalid/d", "at": {"phase": "section", "index": 1}})
        except run.StepError as e:
            assert "no record of what the earlier sections typed" in str(e), str(e)
        else:
            raise AssertionError("a resume past the first section went ahead with no record of the earlier ones")
        assert run.resumption({"url": "https://example.invalid/d", "at": {"phase": "wizard", "index": 0}})["kept"] == []
    case("a resume with no private record of the earlier sections is refused", no_record)

    def missing():
        values = run.Values(answers | {"ricevuta": str(tmp / "nope.p7m")}, fields={"x:ruolo.tipo": "titolare"})
        try:
            run.plan(steps(base), values)
        except run.StepError as e:
            assert "no file" in str(e), str(e)
        else:
            raise AssertionError("a missing file passed the plan")
    case("a file that isn't there stops the plan", missing)

    def reviewed():
        filled = {"procedure": "test/allegati", "result": "filled", "sections": [], "total_seconds": 3.0,
                  "url": "https://example.invalid/d", "values": [], "yours": ["Firmare"]}
        folder = tmp / "review"
        (folder / "private").mkdir(parents=True, exist_ok=True)
        decl = [{"section": "Dichiarazione di consapevolezza", "text": "Il sottoscritto dichiara il vero.",
                 "boxes": [{"label": "Per presa visione", "kind": "checkbox", "ticked": False}]}]
        (folder / "private" / "declarations.json").write_text(json.dumps(decl))
        (folder / "private" / "anteprima.pdf").write_bytes(b"%PDF-1.4\nfinto\n")
        page = run.review.portal_review(filled | {"declarations_read": 1, "preview_pdf": True}, folder)
        assert "Il sottoscritto dichiara il vero." in page and "The declarations you will tick" in page
        assert "1 copied word for word from the portal; none ticked" in page, "no check that none is ticked"
        assert "Download the PDF" in page and "The portal&#x27;s own PDF preview copied" in page
        decl[0]["boxes"][0]["ticked"] = True
        (folder / "private" / "declarations.json").write_text(json.dumps(decl))
        page = run.review.portal_review(filled | {"declarations_read": 1}, folder)
        assert "already ticked before you opened them" in page and "Dichiarazione di consapevolezza" in page
        # Nothing copied (the pane, or an older run): the page says so, and never shows a stand-in.
        pane = run.review.portal_review(filled | {"declarations_read": 2}, tmp / "nowhere")
        assert "Declarations: not copied in this run (read in the pane" in pane, "no note for the pane"
        assert "was not copied in this run" in pane and "The declarations you will tick" not in pane
        assert "couldn&#x27;t be copied (the portal said no)" in run.review.portal_review(
            filled | {"preview_note": "the portal said no"}, tmp / "nowhere")
        stopped = run.review.portal_review(filled | {"result": "stopped", "error": "x"}, tmp / "nowhere")
        assert "Declarations" not in stopped and "preview" not in stopped.lower(), "a stopped run reports on what it never reached"
    case("the review page shows the declarations' text and the portal's PDF, or says it has neither", reviewed)

    server.shutdown()
    shutil.rmtree(tmp)
    print(f"{len(ran) - len(failed)}/{len(ran)} passed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
