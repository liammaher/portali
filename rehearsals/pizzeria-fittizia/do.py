#!/usr/bin/env python3
"""One command per pratica of the Pizzeria Fittizia rehearsal, from its made-up vault.

  do.py scia [--close]
      The SCIA somministrazione on the SUAP portal: a Playwright window opens (log in
      with SPID there if it asks), the runner fills and saves every section, and the
      draft is left open for the person's own steps (declarations, signed attachments,
      payment, sending).

  do.py tari
      The TARI Mod. 5 (iscrizione non domestica): the form filled from the vault and read
      back, opened for review, and the route printed: who signs, where it goes, what to
      attach. Signing and sending are the person's.

Each starts the made-up business vault (fixture.json, as `go test` serves it), reads what
the pratica needs, and stops the vault before the person's steps. Output goes to
~/.cache/pratica-rehearsal/pizzeria-fittizia/<pratica>/. The library is
$PRATICHE_NAPOLI_DIR (for tari). Nothing is ever sent.
"""

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
SKILL = REPO / "claude" / "skills" / "pratica"
PORTAL_PY = Path.home() / ".cache" / "pratica-portal" / "venv" / "bin" / "python"
OUT = Path.home() / ".cache" / "pratica-rehearsal" / "pizzeria-fittizia"
TARI = "napoli/comune/tari-iscrizione-non-domestica"


def serve(work, tokens):
    """The made-up vault over HTTP with these tokens; returns the harness and server.json."""
    shutil.copy(HERE / "fixture.json", work / "fixture.json")
    (work / "needs.json").write_text(json.dumps({"tokens": tokens}))
    log = open(work / "harness.log", "w")
    harness = subprocess.Popen(
        ["go", "test", "./internal/api", "-run", "^TestPraticaPilotVault$", "-count=1", "-timeout", "15m"],
        cwd=REPO, env={**os.environ, "PVAULT_PILOT_DIR": str(work)}, stdout=log, stderr=subprocess.STDOUT)
    while not (work / "server.json").exists():
        if harness.poll() is not None:
            sys.exit("the vault didn't start:\n" + (work / "harness.log").read_text()[-800:])
        time.sleep(0.1)
    return harness, work / "server.json"


def stop(work, harness):
    (work / "done").touch()
    harness.wait()


def scia(work, out, close):
    if not PORTAL_PY.exists():
        sys.exit(f"no runner environment: python3 -m venv {PORTAL_PY.parent.parent} && "
                 f"{PORTAL_PY.parent}/pip install pyyaml playwright && "
                 f"{PORTAL_PY} -m playwright install chromium")
    tokens = json.loads((HERE / "needs.json").read_text())["tokens"]
    harness, server = serve(work, tokens)
    cmd = [str(PORTAL_PY), "-I", str(SKILL / "portal" / "run.py"), "run",
           "--steps", str(HERE / "portal-steps.yaml"), "--answers", str(HERE / "answers.json"),
           "--server", str(server), "--token", "scia", "--out", str(out)] + (["--close"] if close else [])
    run = subprocess.Popen(cmd, stdout=subprocess.PIPE, text=True)
    try:
        for line in run.stdout:
            # run.py reads every value before it opens the browser: the vault can go.
            if harness.poll() is None:
                stop(work, harness)
            print(line, end="", flush=True)
    finally:
        if harness.poll() is None:
            stop(work, harness)
    return run.wait()


def tari(work, out):
    lib = os.environ.get("PRATICHE_NAPOLI_DIR")
    if not lib:
        sys.exit("set PRATICHE_NAPOLI_DIR to a pratiche-napoli checkout")
    fill = [sys.executable, "-I", str(SKILL / "fill" / "fill.py")]
    source = ["--library", lib, "--procedure", TARI]
    needs = subprocess.run(fill + ["needs", *source, "--form", "mod-5", "--for", "gennaro"],
                           check=True, capture_output=True, text=True).stdout
    harness, server = serve(work, json.loads(needs)["tokens"])
    try:
        run = subprocess.run(fill + ["run", *source, "--form", "mod-5", "--for", "gennaro", "--server", str(server),
                                     "--answers", str(SKILL / "fill" / "pilot" / "mod5-answers.json"),
                                     "--out", str(out)])
        route = subprocess.run(fill + ["route", *source, "--server", str(server)])
    finally:
        stop(work, harness)
    if run.returncode == 0 and (out / "filled.pdf").exists():
        subprocess.run(["open", str(out / "filled.pdf")])
    return run.returncode or route.returncode


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("pratica", choices=["scia", "tari"])
    ap.add_argument("--close", action="store_true", help="scia: close the window at the end")
    a = ap.parse_args()
    out = OUT / a.pratica
    shutil.rmtree(out, ignore_errors=True)
    out.mkdir(parents=True)
    # A short path: the vault's socket must fit in 104 bytes.
    work = Path(tempfile.mkdtemp(prefix="fz-", dir="/tmp"))
    t0 = time.monotonic()
    try:
        code = scia(work, out, a.close) if a.pratica == "scia" else tari(work, out)
    finally:
        shutil.rmtree(work, ignore_errors=True)
    print(f"{a.pratica}: {'done' if code == 0 else 'stopped'} in {time.monotonic() - t0:.0f} s; output in {out}")
    return code


if __name__ == "__main__":
    sys.exit(main())
