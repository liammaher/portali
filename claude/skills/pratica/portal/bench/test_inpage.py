#!/usr/bin/env python3
"""Offline tests of inpage.js on a fake portal: no network, no vault, no model.

  ~/.cache/pratica-portal/venv/bin/python -I claude/skills/pratica/portal/bench/test_inpage.py

A local server plays the portal (portal.html, an attachments screen): it stores what Salva
and the upload boxes send and shows it again on every load, so the runner's fresh load of
the draft reads what the "portal" kept. Each case runs run.py's page scripts in headless
Chromium, as the browser pane runs them, and checks the report and the bytes that arrived.
"""

import hashlib
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
    """What the fake portal stored. drop: it shows Denominazione after Salva but never stores it."""

    def reset(self, drop=False):
        self.fields, self.files, self.drop, self.sent = {}, [], drop, False


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
            self.send((HERE / "portal.html").read_bytes(), "text/html; charset=utf-8")
        elif path.startswith("/files/") and path[7:] in LINKED:
            self.send(LINKED[path[7:]], "application/pdf")
        elif path == "/state":
            self.send(json.dumps({"fields": PORTAL.fields, "files": [f["name"] for f in PORTAL.files]}).encode())
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
                                     "sha256": hashlib.sha256(data).hexdigest()})
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


def fill(browser, base, st, answers, files_limit=None, resume=None, urls=None, sizes=None):
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
        return report
    finally:
        page.close()


def main():
    from playwright.sync_api import sync_playwright
    tmp = Path(tempfile.mkdtemp(prefix="inpage-bench-"))
    planimetria, ricevuta = tmp / "planimetria.pdf", tmp / "ricevuta-asl.pdf.p7m"
    planimetria.write_bytes(b"%PDF-1.4\n" + os.urandom(3 * 1024 * 1024))
    ricevuta.write_bytes(b"finta ricevuta firmata\n")
    sha = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in (planimetria, ricevuta)}
    answers = {"den": "PIZZERIA FITTIZIA DI GENNARO FITTIZIO", "planimetria": str(planimetria),
               "ricevuta": str(ricevuta), "impresa": "EDIL FITTIZIA DI MARCO ESEMPIO"}
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

        def theirs():
            PORTAL.reset()
            stops(fill(browser, base, steps(base, save="Firma e invio"), answers), "refused to click")
        case("the person's Firma e invio is never clicked", theirs)


        def recovered():
            # The portal renamed a label: the run stops there and says so, keeping what the
            # first section typed; a fix for that one field resumes it on the same draft,
            # without redoing the first section, and the fresh load checks both.
            PORTAL.reset()
            st = steps(base, start="/portal.html?rename=1")
            first = fill(browser, base, st, answers)
            assert first["result"] == "stopped" and "expected the label Impresa esecutrice" in first["error"], first
            at = first["at"]
            assert (at["phase"], at["index"], at["field"]) == ("section", 1, 0), at
            assert [k["section"] for k in first["kept"]] == [0], first.get("kept")
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
            first = fill(browser, base, st, answers)
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

    def missing():
        values = run.Values(answers | {"ricevuta": str(tmp / "nope.p7m")}, fields={"x:ruolo.tipo": "titolare"})
        try:
            run.plan(steps(base), values)
        except run.StepError as e:
            assert "no file" in str(e), str(e)
        else:
            raise AssertionError("a missing file passed the plan")
    case("a file that isn't there stops the plan", missing)

    server.shutdown()
    shutil.rmtree(tmp)
    print(f"{len(ran) - len(failed)}/{len(ran)} passed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
