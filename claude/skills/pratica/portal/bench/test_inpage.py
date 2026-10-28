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


class Portal:
    """What the fake portal stored. drop: it shows Denominazione after Salva but never stores it."""

    def reset(self, drop=False):
        self.den, self.files, self.drop, self.sent = "", [], drop, False


PORTAL = Portal()


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
        elif path == "/state":
            self.send(json.dumps({"den": PORTAL.den, "files": [f["name"] for f in PORTAL.files]}).encode())
        else:
            self.send_error(404)

    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        if self.path == "/save" and not PORTAL.drop:
            PORTAL.den = json.loads(body)["den"]
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
    return {"procedure": "test/allegati", "start": base + start, "logged_in": base + "/", "wizard": [],
            "sections": [sec]}


def fill(browser, base, st, answers, files_limit=None):
    values = run.Values(answers, fields={"x:ruolo.tipo": "titolare"})
    install, run_js = run.page_scripts(st, values, files_limit)
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
               "ricevuta": str(ricevuta)}
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
            assert PORTAL.den == answers["den"], f"stored {PORTAL.den!r}"
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
