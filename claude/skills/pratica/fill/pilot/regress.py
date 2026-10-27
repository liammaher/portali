#!/usr/bin/env python3
"""Cases the Mod. 1 pilot must get right, good and bad.

  PRATICHE_NAPOLI_DIR=<pratiche-napoli checkout> regress.py [--keep DIR] [case ...]

Each case changes the pilot's inputs (the made-up vault, the answers, the
library entry, the library's layout, the form), runs fill.py against its own synthetic
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
SLUG = "tari-iscrizione-uso-abitativo-2026"


def load(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def without(*ids):
    return lambda fx: fx.update(fields=[f for f in fx["fields"] if f[0] not in ids])


def nobody(fx):
    """The vault's owner alone: no other person in it."""
    fx["fields"] = [f for f in fx["fields"] if ":" not in f[0].split(".", 1)[0]]


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
                        b["table"]["columns"][value[0]] = value[1]
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


CONVIVENTI = "Conviventi (nominativo, luogo e data di nascita, rapporto, codice fiscale)"
COABITANTI = "Coabitanti fuori dallo stato di famiglia (nominativo, luogo e data di nascita, rapporto, codice fiscale)"
SARA = {"Nominativo": "Bruni Sara", "Luogo e data di nascita": "Salerno, 02/02/1990",
        "Rapporto di parentela o coabitante": "coabitante", "Codice fiscale": "BRNSRA90B42H703X"}
LONG_NOTE = ("Contratto di locazione registrato presso l'Agenzia delle Entrate; conviventi come da stato di "
             "famiglia; il box auto non è pertinenza dell'abitazione e sarà dichiarato a parte se necessario.")

# name: (description, changes, expected exit, lines the output must contain)
CASES = {
    "baseline": ("the pilot as it is", {}, 0, ["ok   the form is ready", "opens pratiche.",
                                                "the deadline shows in expiring: 2026-11-30", "a closed pratica leaves expiring"]),
    "coabitante": ("someone outside the stato di famiglia lives there: a row of the table, and a 4th occupant",
                   {"answers": {COABITANTI: [SARA]}}, 1,
                   ["ok   the form is ready", "values read back in their boxes",
                    "FAIL the ente's tari.componenti_tassati matches what was declared: 3 vs 4"]),
    "coabitante-as-text": ("coabitanti answered as text, not one entry per person by column",
                           {"answers": {COABITANTI: "Sara Bruni"}}, 1, ["answer with one entry per person"]),
    "titolo-altro": ("a comodatario ticks altro and writes comodatario next to it",
                     {"fixture": setting({"abitazione.titolo": ["comodatario"]})}, 0, ["values read back in their boxes"]),
    "socket-draft": ("a draft over the vault's socket, as on a real vault: filled, nothing written",
                     {"socket": True}, 0, ["ok   the form is ready", "draft: nothing written to the vault", "no pratiche fields in the vault"]),
    "skill-draft": ("as the pratica skill runs it: the library checkout, the form already downloaded, a draft",
                    {"socket": True, "library": True}, 0,
                    ["ok   form is the version the library maps", "ok   the form is ready",
                     "draft: nothing written to the vault", "no pratiche fields in the vault"]),
    "socket-not-private": ("the socket's run folder isn't private: the token isn't sent",
                           {"socket": True, "chmod_run": 0o755}, 1, ["must be a private folder of yours"]),
    "convivente-gaps": ("a convivente lacks a date of birth, another a codice fiscale",
                        {"fixture": without("giulia:identity.date_of_birth", "luca:identity.codice_fiscale")}, 1,
                        ["giulia:identity.date_of_birth not in the vault", "luca:identity.codice_fiscale not in the vault",
                         "FAIL the form is ready"]),
    "owner-no-cf": ("the owner's codice fiscale isn't in the vault",
                    {"fixture": without("identity.codice_fiscale")}, 1,
                    ["identity.codice_fiscale not in the vault", "FAIL the form is ready"]),
    "domicilio-no-civico": ("a domicilio without a civico: the address must not lose it",
                            {"fixture": with_fields(["domicilio.indirizzo", "Via Chiaia"], ["domicilio.comune", "Napoli"])}, 1,
                            ["domicilio.civico not in the vault"]),
    "hidden-field": ("a field above the token's tier is hidden, not missing",
                     {"fixture": setting({"abitazione.catasto_foglio": ["12", "critical"]})}, 1,
                     ["abitazione.catasto_foglio in the vault but this token can't read it"]),
    "no-intestatario": ("conviventi but no intestatario della scheda",
                        {"fixture": without("famiglia.intestatario_scheda")}, 1,
                        ["famiglia.intestatario_scheda not in the vault"]),
    "empty-household": ("nobody else in the vault: the empty table must be confirmed, not assumed",
                        {"fixture": nobody}, 1, ["Nessuno nello stato di famiglia del vault"]),
    "empty-household-confirmed": ("nobody else, confirmed: the form is ready, and the Comune's 3 occupants don't match 1",
                                  {"fixture": nobody, "answers": {CONVIVENTI: "Nessuno."}}, 1,
                                  ["ok   the form is ready", "FAIL the ente's tari.componenti_tassati matches what was declared: 3 vs 1"]),
    "member-no-stato": ("a person without famiglia.stato_famiglia: asked, not read as no",
                        {"fixture": without("luca:famiglia.stato_famiglia")}, 1,
                        ["luca:famiglia.stato_famiglia not in the vault"]),
    "owner-born-abroad": ("the owner was born abroad (EE) and has no birth country: needed after all",
                          {"fixture": setting({"identity.birth_province": ["EE"]})}, 1,
                          ["identity.birth_country not in the vault (needed: identity.birth_province is EE)"]),
    "member-born-abroad": ("a convivente born abroad, known only from her CF (Z114), without a birth country",
                           {"fixture": without("giulia:identity.birth_country", "giulia:identity.birth_province")}, 1,
                           ["giulia:identity.birth_country not in the vault (needed: *:identity.codice_fiscale matches ^.{11}Z)"]),
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
                       {"layout": box(CONVIVENTI, column=("Codice fiscale", [300, 435]))}, 1,
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


def written_pratiche(server):
    """What the run left in the vault's pratiche, said as a line to look for."""
    req = urllib.request.Request(server["url"] + "/vault/fields", headers={"Authorization": "Bearer " + server["tokens"]["prepare"]})
    with urllib.request.urlopen(req, timeout=15) as resp:
        ids = [f["id"] for f in json.load(resp) if f["id"].startswith("pratiche.")]
    return "\nno pratiche fields in the vault\n" if not ids else f"\npratiche fields in the vault: {ids}\n"


def token_commands():
    """The command for each kind of vault folder, and the Keychain service run
    then reads: one per vault, quoted for the shell."""
    sys.path.insert(0, os.path.dirname(FILL))
    sys.dont_write_bytecode = True  # no __pycache__ in the skill's folder
    import fill
    home = os.path.expanduser("~")
    token = {"scope": "*:identity.*,abitazione.*", "access": "read"}
    tail = "create-service-token pratica-fill --scope '*:identity.*,abitazione.*' --access read --ttl 1h --keychain "
    want = {
        "~/.pvault": "pvault " + tail + "pvault-pratica-fill",
        home + "/.pvault/": "pvault " + tail + "pvault-pratica-fill",
        "~/.pvault-studio": "pvault --vault studio " + tail + "pvault-studio-pratica-fill",
        "/srv/my vault": "VAULT_DIR='/srv/my vault' pvault " + tail + "pvault-my-vault-pratica-fill",
    }
    bad = []
    for folder, command in want.items():
        got = fill.token_command(folder, token)
        if got != command:
            bad.append(f"{folder}: {got}")
        if fill.default_keychain(folder) != command.rsplit(" ", 1)[1]:
            bad.append(f"{folder}: run reads {fill.default_keychain(folder)}")
    return bad


def main():
    library =os.environ.get("PRATICHE_NAPOLI_DIR") or sys.exit("set PRATICHE_NAPOLI_DIR to a pratiche-napoli checkout")
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
                                "--form", FORM], check=True, capture_output=True, text=True).stdout
        layout = load(os.path.join(library, "layouts", ID, FORM + ".json"))
        failed = []
        if not args:
            bad = token_commands()
            print(f"{'ok  ' if not bad else 'FAIL'} token-commands: the pvault command needs prints for each vault"
                  + "".join("\n  " + b for b in bad))
            failed += ["token-commands"] if bad else []
        for name in names:
            description, changes, want_exit, want_lines = CASES[name]
            ok, detail = case(base, name, entry, layout, pdf, needs, changes, want_exit, want_lines)
            print(f"{'ok  ' if ok else 'FAIL'} {name}: {description}{'' if ok else chr(10) + detail}")
            if keep:
                shutil.copytree(os.path.join(base, name, "out"), os.path.join(keep, name), dirs_exist_ok=True)
            if not ok:
                failed.append(name)
        total = len(names) + (0 if args else 1)
        print(f"{total - len(failed)}/{total} cases passed")
        return 1 if failed else 0
    finally:
        shutil.rmtree(base, ignore_errors=True)


def case(base, name, entry, layout, pdf, needs, changes, want_exit, want_lines):
    work = os.path.join(base, name)
    out = os.path.join(work, "out")
    os.makedirs(out)
    fixture = load(os.path.join(HERE, "fixture.json"))
    if "fixture" in changes:
        changes["fixture"](fixture)
    entry = copy.deepcopy(entry)
    if "entry" in changes:
        changes["entry"](entry)
    layout = copy.deepcopy(layout)
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
        server = load(os.path.join(work, "server.json"))
        common = [sys.executable, "-I", FILL, "run", "--entry", os.path.join(work, "entry.json"), "--form", FORM,
                  "--layout", os.path.join(work, "layout.json"), "--pdf", os.path.join(work, "form.pdf"), "--out", out,
                  "--answers", os.path.join(work, "answers.json")]
        if changes.get("library"):
            # The form a past run left in the output folder is used, not fetched again.
            shutil.copy(os.path.join(work, "form.pdf"), os.path.join(out, "form.pdf"))
            common = [sys.executable, "-I", FILL, "run", "--library", os.environ["PRATICHE_NAPOLI_DIR"],
                      "--procedure", ID, "--form", FORM, "--out", out, "--answers", os.path.join(work, "answers.json")]
        if changes.get("socket"):
            # As on a real vault: its folder, the token from the environment, a draft.
            if "chmod_run" in changes:
                os.chmod(os.path.join(server["vault_dir"], "run"), changes["chmod_run"])
            run = subprocess.run(common + ["--vault-dir", server["vault_dir"], "--draft"], capture_output=True, text=True,
                                 env={**os.environ, "VAULT_TOKEN": server["tokens"]["prepare"]})
            run.stdout += written_pratiche(server)
        else:
            run = subprocess.run(common + ["--server", os.path.join(work, "server.json"), "--reply", os.path.join(HERE, "reply.json")],
                                 capture_output=True, text=True)
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
