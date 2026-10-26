#!/usr/bin/env python3
"""Cases the Mod. 1 pilot must get right, good and bad.

  PRATICHE_NAPOLI_DIR=<pratiche-napoli checkout> regress.py [--keep DIR] [case ...]

Each case changes the pilot's inputs (the made-up vault, the answers, the
library entry, the layout, the form), runs fill.py against its own synthetic
vault, and checks the exit code and that each expected line is in the output.
The form is downloaded once from the entry's URL. --keep copies each case's
filled.pdf, page-1.png and report.json to DIR/<case>/.
"""

import copy
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", "..", "..", "..", ".."))
FILL = os.path.join(HERE, "..", "fill.py")
ID, FORM = "napoli/comune/tari-iscrizione-uso-abitativo", "mod-1"
LAYOUT = os.path.join(HERE, "..", "layouts", ID, FORM + ".json")
SLUG = "tari-iscrizione-uso-abitativo-2026"


def load(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def without(*ids):
    return lambda fx: fx.update(fields=[f for f in fx["fields"] if f[0] not in ids])


def with_fields(*fields):
    return lambda fx: fx.update(fields=fx["fields"] + [list(f) for f in fields])


def setting(changes):
    """Field values (and a tier as a third item) changed by ID."""
    def edit(fx):
        fx["fields"] = [[f[0], *changes[f[0]]] if f[0] in changes else f for f in fx["fields"]]
    return edit


def row(name, **changes):
    def edit(entry):
        for r in next(f for f in entry["forms"] if f["id"] == FORM)["fields"]:
            if r["label"] == name:
                r.update(changes)
    return edit


def box(label, **changes):
    def edit(layout):
        for b in layout["boxes"]:
            if b.get("label") == label:
                for key, value in changes.items():
                    if key == "column":
                        b["table"]["columns"][value[0]]["x"] = value[1]
                    else:
                        b[key] = value
    return edit


def people(n):
    rows = []
    for i in range(n):
        s = f"p{i}"
        rows += [[f"{s}:famiglia.relazione", "figlio"], [f"{s}:famiglia.stato_famiglia", "si"],
                 [f"{s}:identity.first_name", f"Nome{i}"], [f"{s}:identity.last_name", "Rossi"],
                 [f"{s}:identity.date_of_birth", "2015-06-10"], [f"{s}:identity.birth_comune", "Napoli"],
                 [f"{s}:identity.codice_fiscale", "RSSLCU15H10F839Q"]]
    return with_fields(*rows)


LONG_NOTE = ("Contratto di locazione registrato presso l'Agenzia delle Entrate; conviventi come da stato di "
             "famiglia; il box auto non è pertinenza dell'abitazione e sarà dichiarato a parte se necessario.")

# name: (description, changes, expected exit, lines the output must contain)
CASES = {
    "baseline": ("the pilot as it is", {}, 0, ["the form is ready, so the pratica is opened", "a closed pratica leaves expiring"]),
    "convivente-gaps": ("a convivente lacks a date of birth, another a codice fiscale",
                        {"fixture": without("giulia:identity.date_of_birth", "luca:identity.codice_fiscale")}, 1,
                        ["giulia has no date_of_birth", "luca has no codice_fiscale", "FAIL the form is ready"]),
    "owner-no-cf": ("the owner's codice fiscale isn't in the vault",
                    {"fixture": without("identity.codice_fiscale")}, 1,
                    ["identity.codice_fiscale not in the vault", "FAIL the form is ready"]),
    "domicilio-no-civico": ("a domicilio without a civico: the address must not lose it",
                            {"fixture": with_fields(["domicilio.indirizzo", "Via Chiaia"], ["domicilio.comune", "Napoli"])}, 1,
                            ["Appartamento sito in via: no @civico"]),
    "hidden-field": ("a field above the token's tier is hidden, not missing",
                     {"fixture": setting({"abitazione.catasto_foglio": ["12", "critical"]})}, 1,
                     ["abitazione.catasto_foglio in the vault but this token can't read it"]),
    "no-intestatario": ("conviventi but no intestatario della scheda",
                        {"fixture": without("famiglia.intestatario_scheda")}, 1,
                        ["famiglia.intestatario_scheda not in the vault"]),
    "too-many-people": ("six people in the stato di famiglia, five rows on the form",
                        {"fixture": people(4)}, 1, ["6 people for 5 rows"]),
    "unicode-names": ("names Helvetica can't print",
                      {"fixture": setting({"giulia:identity.last_name": ["D’Angelo"], "giulia:identity.first_name": ["Łucja"]})}, 0,
                      ["values read back in their boxes"]),
    "answers-loosely": ("'No.' skips the garage, 'Sì' ticks uso stagionale and asks for the line it needs",
                        {"answers": {"Autorimessa/cantinola/posto auto (via, dati catastali, superficie, pertinenza)": "No.",
                                     "Richiesta di riduzione del 40% \"Uso stagionale\"": "Sì"}}, 1,
                        ["must be filled by hand", "nothing written outside a box"]),
    "long-note": ("a long note runs onto the continuation lines", {"answers": {"Eventuali note": LONG_NOTE}}, 0,
                  ["values read back in their boxes"]),
    "closed-same-slug": ("a closed pratica already has the slug: this one takes -2",
                         {"fixture": with_fields([f"pratiche.{SLUG}_stato", "respinta"])}, 0,
                         [f"opens pratiche.{SLUG}-2_titolo"]),
    "open-same-slug": ("an open pratica already has the slug: resume it",
                       {"fixture": with_fields([f"pratiche.{SLUG}_titolo", "TARI"])}, 1, ["resume it instead"]),
    "shifted-column": ("the codice fiscale column moved off the table's lines",
                       {"layout": box("Conviventi (nominativo, luogo e data di nascita, rapporto, codice fiscale)", column=(3, [300, 435]))}, 1,
                       ["isn't between the table's lines"]),
    "box-over-label": ("the Via box stretched over the printed n°",
                       {"layout": box("Via", rect=[55, 177.7, 380, 188.2])}, 1, ["covers the printed"]),
    "moved-box": ("the Via box moved 40pt down", {"layout": box("Via", rect=[55, 217.7, 351, 228.2])}, 1,
                  ["isn't on the form's blank line"]),
    "other-form": ("a form whose sha256 isn't the library's", {"pdf": b"\n% changed\n"}, 1,
                   ["FAIL form is the version the library maps"]),
    "renamed-row": ("a row renamed in the library", {"entry": row("Foglio", label="Foglio catastale")}, 1,
                    ["every library row has a place on the form", "every layout box is a library row"]),
    "party-row": ("a row read from a party (business vaults): refused, not read as the owner's",
                  {"entry": row("C.F.", map={"party": {"ruolo": "titolare"}, "direct": "identity.codice_fiscale"})}, 1,
                  ["party and repeat by ruolo aren't supported yet"]),
    "unknown-map": ("a row with a map this tool doesn't know: refused, not left blank",
                    {"entry": row("Sub.", map={"dirct": "abitazione.catasto_subalterno"})}, 1, ["unknown map"]),
    "unanswered": ("no answers to the ask rows (an empty answers map replaces them)", {"answers": {}}, 1,
                   ["'Eventuali note' is answered"]),
}


def main():
    library = os.environ.get("PRATICHE_NAPOLI_DIR") or sys.exit("set PRATICHE_NAPOLI_DIR to a pratiche-napoli checkout")
    args = sys.argv[1:]
    keep = None
    if args[:1] == ["--keep"]:
        keep, args = args[1], args[2:]
    names = args or list(CASES)
    base = tempfile.mkdtemp()
    try:
        entry_json = subprocess.run(
            ["node", "--input-type=module", "-e",
             'import {parse} from "yaml"; import {readFileSync} from "node:fs";'
             'process.stdout.write(JSON.stringify(parse(readFileSync(process.argv[1],"utf8"),{version:"1.2"})))',
             f"procedures/{ID}.yaml"], cwd=library, check=True, capture_output=True, text=True).stdout
        entry = json.loads(entry_json)
        url = next(f["url"] for f in entry["forms"] if f["id"] == FORM)
        with urllib.request.urlopen(url, timeout=60) as resp:
            pdf = resp.read()
        # Every case gets the unchanged entry's tokens: an entry fill.py
        # refuses must be refused by run, which is what a case checks.
        with open(os.path.join(base, "entry.json"), "w", encoding="utf-8") as f:
            f.write(entry_json)
        needs = subprocess.run([sys.executable, "-I", FILL, "needs", "--entry", os.path.join(base, "entry.json"),
                                "--form", FORM, "--layout", LAYOUT], check=True, capture_output=True, text=True).stdout
        failed = []
        for name in names:
            description, changes, want_exit, want_lines = CASES[name]
            ok, detail = case(base, name, entry, pdf, needs, changes, want_exit, want_lines)
            print(f"{'ok  ' if ok else 'FAIL'} {name}: {description}{'' if ok else chr(10) + detail}")
            if keep:
                shutil.copytree(os.path.join(base, name, "out"), os.path.join(keep, name), dirs_exist_ok=True)
            if not ok:
                failed.append(name)
        print(f"{len(names) - len(failed)}/{len(names)} cases passed")
        return 1 if failed else 0
    finally:
        shutil.rmtree(base, ignore_errors=True)


def case(base, name, entry, pdf, needs, changes, want_exit, want_lines):
    work = os.path.join(base, name)
    out = os.path.join(work, "out")
    os.makedirs(out)
    fixture = load(os.path.join(HERE, "fixture.json"))
    if "fixture" in changes:
        changes["fixture"](fixture)
    entry = copy.deepcopy(entry)
    if "entry" in changes:
        changes["entry"](entry)
    layout = load(LAYOUT)
    if "layout" in changes:
        changes["layout"](layout)
    answers = load(os.path.join(HERE, "answers.json"))
    answers["answers"] = {} if changes.get("answers") == {} else {**answers["answers"], **changes.get("answers", {})}
    files = {"fixture.json": fixture, "entry.json": entry, "layout.json": layout, "answers.json": answers}
    for file, data in files.items():
        with open(os.path.join(work, file), "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False)
    with open(os.path.join(work, "form.pdf"), "wb") as f:
        f.write(pdf + changes.get("pdf", b""))
    with open(os.path.join(work, "needs.json"), "w") as f:
        f.write(needs)

    log = open(os.path.join(work, "harness.log"), "w")
    harness = subprocess.Popen(["go", "test", "./internal/api", "-run", "^TestPraticaPilotVault$", "-count=1", "-timeout", "5m"],
                               cwd=REPO, env={**os.environ, "PVAULT_PILOT_DIR": work}, stdout=log, stderr=subprocess.STDOUT)
    try:
        while not os.path.exists(os.path.join(work, "server.json")):
            if harness.poll() is not None:
                with open(os.path.join(work, "harness.log")) as f:
                    return False, "harness: " + f.read()[-800:]
            time.sleep(0.1)
        run = subprocess.run([sys.executable, "-I", FILL, "run", "--entry", os.path.join(work, "entry.json"), "--form", FORM,
                              "--layout", os.path.join(work, "layout.json"), "--server", os.path.join(work, "server.json"),
                              "--pdf", os.path.join(work, "form.pdf"), "--out", out, "--answers", os.path.join(work, "answers.json"),
                              "--reply", os.path.join(HERE, "reply.json"), "--deadline-from", "abitazione.data_inizio",
                              "--deadline-days", "90"], capture_output=True, text=True)
    finally:
        open(os.path.join(work, "done"), "w").close()
        harness.wait()
        log.close()
    output = run.stdout + run.stderr
    lacking = [line for line in want_lines if line not in output]
    if run.returncode != want_exit or lacking:
        return False, f"  exit {run.returncode} (want {want_exit}); lacking {lacking}\n" + "\n".join("  | " + l for l in output.splitlines()[-25:])
    return True, ""


if __name__ == "__main__":
    sys.exit(main())
