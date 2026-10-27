#!/usr/bin/env python3
"""Cases the fill pilots must get right, good and bad.

  PRATICHE_NAPOLI_DIR=<pratiche-napoli checkout> regress.py [--keep DIR] [case ...]

The pilots are four library forms: the TARI Mod. 1 (drawn on, with the
stato di famiglia's table), the ricorso al Prefetto (drawn on, two pages, tick
boxes that are font characters), the CILA Allegato A (fillable fields) and
the Regione Campania scheda anagrafica, in a business vault (signed by its
legale rappresentante, a procuratore read by --party, typed |__| cells).
Each case changes a pilot's inputs (the made-up vault, the answers, the
library entry, the library's layout, the form, who signs), runs fill.py
against its own synthetic vault, and checks the exit code and that each
expected line is in the output, which ends with a "wrote: <text>" line for
each value written. Each form is downloaded once from its entry's URL. --keep
copies each case's filled.pdf, page PNGs and report.json to DIR/<case>/:
`regress.py --keep DIR baseline` is the Mod. 1 pilot end to end, to look at.
PILOT_FORM_PDF gives the Mod. 1 from disk instead (fill.py still checks its
sha256).
"""

import copy
import datetime as dt
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
import types
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", "..", "..", "..", ".."))
FILL = os.path.join(HERE, "..", "fill.py")
LIBRARY = os.environ.get("PRATICHE_NAPOLI_DIR", "")
sys.path.insert(0, os.path.dirname(FILL))
sys.dont_write_bytecode = True  # no __pycache__ in the skill's folder
import fill  # noqa: E402
# name: the procedure, its form, the answers to its asks, the ente's reply if one is played, the made-up vault
PILOTS = {
    "mod-1": ("napoli/comune/tari-iscrizione-uso-abitativo", "mod-1", "answers.json", "reply.json", "fixture.json"),
    "ricorso": ("napoli/comune/verbale-pagamento-ricorso", "ricorso-prefetto", "ricorso-answers.json", None, "fixture.json"),
    "cila": ("napoli/comune/cila-lavori-edilizi", "allegato-a-programma-100", "cila-answers.json", None, "fixture.json"),
    "scheda": ("napoli/comune/cessazione-attivita-commerciale", "scheda-anagrafica", "scheda-answers.json", None,
               "business-fixture.json"),
}
ID, FORM = PILOTS["mod-1"][:2]
SLUG = "tari-iscrizione-uso-abitativo-2026"
RICORSO_SLUG = "verbale-pagamento-ricorso-2026"
LR = "in proprio / in qualità di Legale Rappresentante di"
PENDENZE = ("Dichiara di non avere pendenze verso il Comune di Napoli per IMU, Tassa rifiuti, Imposta di soggiorno, "
            "Canone di occupazione suolo pubblico (fino al 2020) e Canone patrimoniale (dal 2021)")


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


def row(name, choice=None, **changes):
    """An entry change: the row labelled name in any of its forms, or with
    choice, that box of its choices."""
    def edit(entry):
        for r in (r for f in entry["forms"] for r in f.get("fields", [])):
            if r["label"] == name:
                (next(c for c in r["choices"] if c["box"] == choice) if choice else r).update(changes)
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


GIULIA_HOME = [["giulia:residenza.indirizzo", "Via Chiaia"], ["giulia:residenza.civico", "12"],
               ["giulia:residenza.comune", "Napoli"], ["giulia:residenza.provincia", "NA"],
               ["giulia:identity.phone", "+39 333 111 1111"]]
LONG_MOTIVI = " ".join(["Il veicolo era regolarmente parcheggiato nell'area di sosta a pagamento con ticket valido, "
                        "esposto sul cruscotto e allegato in copia, come risulta dalla ricevuta del parcometro."] * 9)
def deadline_from(field, records):
    """An entry change: a deadline counted from field, and records the ente's reply fills."""
    def edit(entry):
        entry.setdefault("deadline", {}).setdefault("rules", []).append({"compute": {"from": field, "days": 0}})
        entry["records"] = [{"field": f, "from": "la risposta"} for f in records]
    return edit


PROCURA = "3 – Procura conferita (dati del procuratore/delegato)?"
AS_PROCURATORE = "in qualità di: Procuratore/delegato / Agenzia per le imprese"
WITH_PROCURA = {PROCURA: "sì", AS_PROCURATORE: "procuratore"}
SOCI_ALLEGATO = [row(f"{n} (procuratore)", map={"repeat": {"ruolo": "socio", "except": "dichiarante"}, "fields": [f]})
                 for n, f in (("Cognome", "*:identity.last_name"), ("Nome", "*:identity.first_name"))]
OWNER_BUSINESS = [["business.denominazione", "Ditta Mario Rossi"], ["business.forma_giuridica", "ditta_individuale"],
                  ["business.codice_fiscale", "RSSMRA80A01F839W"], ["business.qualita_dichiarante", "titolare"],
                  ["business.sede_legale_comune", "Napoli"], ["business.sede_legale_provincia", "NA"],
                  ["business.sede_legale_indirizzo", "Via Toledo"], ["business.sede_legale_civico", "100"],
                  ["business.sede_legale_cap", "80134"], ["business.telefono", "+39 333 000 0000"],
                  ["business.pec", "mario.rossi@pec.example.it"], ["business.cciaa", "NA"], ["business.rea", "NA-7654321"],
                  ["identity.citizenship", "italiana"]]


def no_soci(fx):
    """The business with no soci but its legale rappresentante."""
    fx["fields"] = [f for f in fx["fields"] if not f[0].startswith(("verdi:", "gialli:"))]


def both(*edits):
    """Several changes of one kind, in order."""
    def edit(x):
        for e in edits:
            e(x)
    return edit


CONVIVENTI = "Conviventi (nominativo, luogo e data di nascita, rapporto, codice fiscale)"
COABITANTI = "Coabitanti fuori dallo stato di famiglia (nominativo, luogo e data di nascita, rapporto, codice fiscale)"
SARA = {"Nominativo": "Bruni Sara", "Luogo e data di nascita": "Salerno, 02/02/1990",
        "Rapporto di parentela o coabitante": "coabitante", "Codice fiscale": "BRNSRA90B42H703X"}
LONG_NOTE = ("Contratto di locazione registrato presso l'Agenzia delle Entrate; conviventi come da stato di "
             "famiglia; il box auto non è pertinenza dell'abitazione e sarà dichiarato a parte se necessario.")

PILOT_PASSWORD = "test-password-123"  # the synthetic vault's, internal/api testPassword


def review_signing(server, out, layout):
    """What the user does with pvault sign review, over the vault's socket: a
    sign grant for the request, the signed PDF, a check that the signature and
    the date landed in the layout's boxes and nowhere else, the approval.
    Returns lines like fill.py's checks."""
    import fitz
    lines = []

    def ok(name, good, detail=""):
        lines.append(f"{'ok  ' if good else 'FAIL'} {name}{': ' + detail if detail and not good else ''}")
        return good

    sock = os.path.join(server["vault_dir"], "run", "pvault.sock")

    def call(method, path, body=None, token=None):
        conn = fill.UnixHTTPConnection(sock, 15)
        headers = {"Host": "localhost", "Content-Type": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        conn.request(method, path, body=json.dumps(body).encode() if body is not None else None, headers=headers)
        resp = conn.getresponse()
        data = resp.read()
        conn.close()
        return resp.status, {k.lower(): v for k, v in resp.getheaders()}, data

    signing = load(os.path.join(out, "report.json")).get("signing")
    if not ok("the report names the signing request", bool(signing)):
        return "\n" + "\n".join(lines)
    sid = signing["id"]
    status, _, _ = call("POST", f"/vault/signing/{sid}/approve", {"sha256": "0" * 64}, server["tokens"]["prepare"])
    ok("the agent's token can't approve", status == 403, str(status))
    status, _, _ = call("GET", f"/vault/signing/{sid}/candidate", token=server["tokens"]["prepare"])
    ok("the agent's token can't read the signed file", status == 403, str(status))

    status, _, data = call("POST", "/vault/session", {"password": PILOT_PASSWORD, "sign": sid})
    if not ok("the user's password gives a sign grant", status == 200, f"{status} {data[:80]!r}"):
        return "\n" + "\n".join(lines)
    grant = json.loads(data)["token"]
    status, headers, signed = call("GET", f"/vault/signing/{sid}/candidate", token=grant)
    if not ok("the sign grant reads the signed file", status == 200, str(status)):
        return "\n" + "\n".join(lines)
    digest = hashlib.sha256(signed).hexdigest()
    ok("its hash is the one the vault announced", digest == headers.get("x-signing-sha256"))
    signed_pdf = os.path.join(out, "signed.pdf")
    with open(signed_pdf, "wb") as f:
        f.write(signed)

    before, after = fitz.open(os.path.join(out, "filled.pdf")), fitz.open(signed_pdf)
    ok("the signed file has the form's pages", len(after) == len(before), f"{len(after)} vs {len(before)}")
    boxes = [(s["page"] if "page" in s else 1, k, s[k]) for s in layout["signatures"] for k in ("rect", "date_rect") if k in s]
    scale = 2  # 144 dpi
    stray, touched = 0, {k: 0 for _, k, _ in boxes}
    for pno in range(len(before)):
        a = before[pno].get_pixmap(dpi=72 * scale)
        b = after[pno].get_pixmap(dpi=72 * scale)
        mine = [(k, r) for p, k, r in boxes if p == pno + 1]
        for y in range(a.height):
            for x in range(a.width):
                if a.pixel(x, y) == b.pixel(x, y):
                    continue
                inside = [k for k, (x0, top, x1, bottom) in mine
                          if x0 * scale - 3 <= x <= x1 * scale + 3 and top * scale - 3 <= y <= bottom * scale + 3]
                if inside:
                    touched[inside[0]] += 1
                else:
                    stray += 1
    ok("the signature and the date changed no pixel outside their boxes", stray == 0, f"{stray} pixels")
    for k, n in touched.items():
        ok(f"something was drawn in the {k}", n > 20, f"{n} pixels")
    page, _, rect = next((p, k, r) for p, k, r in boxes if k == "date_rect") if any(k == "date_rect" for _, k, _ in boxes) else (0, "", None)
    if rect:
        want = dt.date.today().strftime("%d/%m/%Y")
        words = [w[4] for w in after[page - 1].get_text("words", clip=fitz.Rect(*rect) + (-2, -2, 2, 2))]
        ok("the date is today's, written by the vault", want in words, f"{words!r} vs {want!r}")

    status, _, data = call("POST", f"/vault/signing/{sid}/approve", {"sha256": "0" * 64}, grant)
    ok("a wrong hash approves nothing", status == 400, f"{status}")
    status, _, data = call("POST", f"/vault/signing/{sid}/approve", {"sha256": digest}, grant)
    ok("the user approves the file they reviewed", status == 200, f"{status} {data[:80]!r}")
    status, _, _ = call("GET", f"/vault/signing/{sid}", token=server["tokens"]["prepare"])
    ok("the agent can see it was approved", status == 200)
    return "\n" + "\n".join(lines)


# name: (description, changes, expected exit, lines the output must contain)
CASES = {
    "baseline": ("the pilot as it is", {}, 0, ["ok   the form is ready", "opens pratiche.",
                                                "the deadline shows in expiring: 2026-11-30", "a closed pratica leaves expiring",
                                                "wrote: 01/01/1980", "wrote: Toledo", "wrote: Toledo, 100, int. 5, piano 3"]),
    "sign": ("--sign files a request; the user's review stamps the signature and date in their boxes only",
             {"sign": True}, 0,
             ["ok   the form is ready", "is waiting for the user", "ok   the report names the signing request",
              "ok   the agent's token can't approve", "ok   the agent's token can't read the signed file",
              "ok   the user's password gives a sign grant", "ok   the sign grant reads the signed file",
              "ok   the signature and the date changed no pixel outside their boxes",
              "ok   something was drawn in the rect", "ok   something was drawn in the date_rect",
              "ok   the date is today's, written by the vault", "ok   a wrong hash approves nothing",
              "ok   the user approves the file they reviewed"]),
    "sign-again": ("--sign twice: the second run finds the pratica open and files only the signing request",
                   {"sign": True, "no_reply": True, "again": True}, 0,
                   ["is already open: only the signing request was filed", "is waiting for the user"]),
    "route-pec": ("route --channel pec: the shared steps and the PEC's own, how it signs, the proof",
                  {"route": "pec"}, 0,
                  ["via PEC (other routes:", "dalla propria PEC a entrate.tari-tares-tarsu@pec.comune.napoli.it",
                   "Signing (Mod. 1", "Proof: legal receipt", "Pagare l'F24 alle scadenze"]),
    "route-post": ("route --channel post: the raccomandata A/R and the law's signing rule for post",
                   {"route": "post"}, 0,
                   ["con raccomandata A/R a Comune di Napoli", "Modulo firmato a mano in originale, con la fotocopia"]),
    "route-desk": ("route --channel desk: the URP of the Municipalità the vault places the home in",
                   {"route": "desk"}, 0,
                   ["Your office: Sede della Municipalità 2", "Piazza Dante 93"]),
    "route-entry-only": ("route --entry without --library: the law's signing rule is the library's, so it says to pass it",
                         {"route": "post", "entry_only": True}, 0,
                         ["by post (other routes:", "contract/signing.json: pass --library"]),
    "route-portal": ("route --channel portal: the form's own signing for the portal (its login)",
                     {"route": "portal"}, 0, ["online (other routes:", "login, unclear whether an ID copy is needed"]),
    "route-unknown": ("route --channel fax: a route the entry doesn't have",
                      {"route": "fax"}, 1, ["has no fax channel; it has desk, email, pec, portal, post"]),
    "sign-channel-post": ("--channel post --sign: the raccomandata takes a handwritten signature too",
                          {"sign": True, "no_reply": True, "channel": "post"}, 0,
                          ["is waiting for the user", f"ok   opens pratiche.{SLUG}_canale"]),
    "sign-channel-portal": ("--channel portal --sign: the login is the signature, so there is nothing to stamp",
                            {"sign": True, "no_reply": True, "channel": "portal"}, 1,
                            ["FAIL the library lists a handwritten signature for portal"]),
    "channel-unknown": ("--channel with a type the entry doesn't have",
                        {"sign": True, "no_reply": True, "channel": "fax"}, 1,
                        ["FAIL the entry has a fax channel for this procedure"]),
    "sign-no-handwritten": ("the library lists no handwritten signature for the channel: no request",
                            {"sign": True, "entry": lambda e: next(f for f in e["forms"] if f["id"] == FORM)
                             .update(signing=[{"channel": "pec", "ways": ["digital"], "id_copy": "required", "status": "unverified"}])},
                            1, ["FAIL the library lists a handwritten signature for pec"]),
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
    "no-cf-ne": ("a convivente with no codice fiscale: the user's N/E fills the box, the vault untouched",
                 {"fixture": without("luca:identity.codice_fiscale"),
                  "values": {"luca:identity.codice_fiscale": "N/E"}}, 0,
                 ["ok   luca:identity.codice_fiscale from answers.json, not the vault: N/E",
                  "values read back in their boxes", "ok   the form is ready"]),
    "value-over-vault": ("an answers.json value for a field the vault has: refused, the vault decides",
                         {"values": {"luca:identity.codice_fiscale": "N/E"}}, 1,
                         ["FAIL answers.json value luca:identity.codice_fiscale is not in the vault: the vault has it"]),
    "value-not-read": ("an answers.json value for a field the form doesn't read: refused",
                       {"values": {"luca:identity.shoe_size": "42"}}, 1,
                       ["FAIL answers.json value luca:identity.shoe_size is a field this form reads"]),
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
    "party-row": ("a row read from a party nobody was named for: asked, never read as the owner's",
                  {"entry": row("C.F.", map={"party": {"ruolo": "titolare"}, "direct": "identity.codice_fiscale"})}, 1,
                  ["'C.F.' is answered: whose data? --party titolare=<slug>"]),
    "unknown-map": ("a row with a map this tool doesn't know: refused, not left blank",
                    {"entry": row("Sub.", map={"dirct": "abitazione.catasto_subalterno"})}, 1, ["unknown map"]),
    "unanswered": ("no answers to the ask rows (an empty answers map replaces them)", {"answers": {}}, 1,
                   ["'Eventuali note' is answered"]),
    "mod1-for-giulia": ("Mod. 1 for someone else: its table is the intestatario's, so --for is refused",
                        {"for": "giulia"}, 1, ["signed by the intestatario"]),

    "ricorso": ("the ricorso al Prefetto as it is: two pages, its tick boxes font characters", {"pilot": "ricorso"}, 0,
                ["ok   every layout box sits on the form's own blank", "ok   the form is ready", f"opens pratiche.{RICORSO_SLUG}_titolo",
                 "wrote: Rossi Mario", "wrote: 14/09/2026", "wrote: 01/01/1980", "wrote: X"]),
    "ricorso-long-motivi": ("motivi longer than page 1's lines run onto page 2", {"pilot": "ricorso", "answers": {
        "propone RICORSO per i seguenti motivi": LONG_MOTIVI}}, 0, ["values read back in their boxes", "page 2: "]),
    "ricorso-for-giulia": ("filed by Giulia (--for giulia): her name, birth and home, and the pratica is hers",
                           {"pilot": "ricorso", "for": "giulia", "fixture": with_fields(*GIULIA_HOME)}, 0,
                           ["for giulia", "ok   giulia is a person in the vault", "wrote: Bianchi Giulia", "wrote: Londra",
                            "wrote: 15/03/1982", "wrote: Chiaia", f"opens pratiche.{RICORSO_SLUG}_persona"]),
    "ricorso-for-giulia-gaps": ("filed by Giulia, who has no residenza in the vault: asked, never the owner's",
                                {"pilot": "ricorso", "for": "giulia"}, 1,
                                ["giulia:residenza.comune not in the vault", "giulia:identity.phone not in the vault"]),
    "ricorso-box-missing": ("a tick box moved off the form's box character", {"pilot": "ricorso", "layout": box(
        "in qualità di conducente / proprietario", choices={"conducente": [100, 391.0, 106, 399.0], "proprietario": [176.5, 391.0, 182.5, 399.0]})},
                            1, ["no checkbox at [100, 391.0, 106, 399.0]"]),
    "ricorso-no-page": ("a continuation line on a page the form doesn't have: reported, not a crash", {"pilot": "ricorso", "layout": box(
        "propone RICORSO per i seguenti motivi", continuation=[{"page": 3, "rect": [48.3, 58.3, 540.3, 69.3]}])}, 1,
                        ["on page 3, which the form doesn't have", "page 3: the form has 2 pages", "checks passed"]),
    "ricorso-for-giulia-deadline": ("filed by Giulia: a deadline from her own date, and the ente's records saved as hers",
                                    {"pilot": "ricorso", "for": "giulia", "needs_from_entry": True,
                                     "entry": deadline_from("cie.data_scadenza", ["cie.numero"]),
                                     "fixture": with_fields(*GIULIA_HOME, ["cie.data_scadenza", "2028-01-01"],
                                                            ["giulia:cie.data_scadenza", "2027-03-01"]),
                                     "reply": {"records": {"cie.numero": "CA12345AB"}}}, 0,
                                    ["the deadline shows in expiring: 2027-03-01", "ok   track sets giulia:cie.numero",
                                     "a closed pratica leaves expiring"]),
    "ricorso-for-giulia-value": ("filed by Giulia, her phone given in answers.json unprefixed: it is hers",
                                 {"pilot": "ricorso", "for": "giulia", "fixture": with_fields(*GIULIA_HOME[:4]),
                                  "values": {"identity.phone": "+39 333 222 2222"}}, 0,
                                 ["ok   giulia:identity.phone from answers.json, not the vault: +39 333 222 2222",
                                  "wrote: +39 333 222 2222"]),
    "ricorso-not-address": ("a box after a printed Via that isn't an address keeps its first word",
                            {"pilot": "ricorso", "entry": row("Via", map={"ask": "Via?"}), "answers": {"Via": "Via Crucis 3"}}, 0,
                            ["wrote: Via Crucis 3"]),

    "cila": ("the CILA Allegato A as it is: its own fillable fields, in proprio", {"pilot": "cila"}, 0,
             ["ok   every layout box sits on the form's own blank", "ok   the form is ready", "wrote: Rossi Mario",
              "wrote: 01/01/1980", "wrote: X", "wrote: Napoli, 27/10/2026"]),
    "cila-legale-rappresentante": ("as legale rappresentante: the business's name and codice fiscale are needed",
                                   {"pilot": "cila", "answers": {LR: "legale rappresentante"}}, 1,
                                   ["business.denominazione not in the vault", "business.codice_fiscale not in the vault"]),
    "cila-unknown-answer": ("an answer that ticks neither in proprio nor legale rappresentante: reported, not left blank",
                            {"pilot": "cila", "answers": {LR: "legale rapp."}}, 1, ["ticks none of its boxes"]),
    "cila-write-widget": ("a box with write_value whose text goes in one of the form's fillable fields",
                          {"pilot": "cila", "entry": row(PENDENZE, choice="dichiara", write_value=True),
                           "layout": box(PENDENZE, write={"dichiara": "Text Box 1_10"})}, 0,
                          ["ok   the form is ready", "wrote: sì"]),
    "scheda": ("a business vault's scheda anagrafica, signed by its legale rappresentante, no procura",
               {"pilot": "scheda", "for": "rossi"}, 0,
               ["ok   rossi signs as titolare or legale rappresentante", "ok   every layout box sits on the form's own blank",
                "ok   the form is ready", "wrote: Rossi", "wrote: R", "wrote: 0", "wrote: Italia", "wrote: X", "wrote: 1",
                "wrote: 6", "wrote: Pizzeria Esempio SRL", "wrote: Via dei Tribunali", "page 2: "]),
    "scheda-no-signer": ("a business vault's form without --for: refused, naming who can sign", {"pilot": "scheda"}, 1,
                         ["pass --for to needs and run, one of rossi"]),
    "scheda-socio-signs": ("a socio who isn't legale rappresentante can't sign for the business",
                           {"pilot": "scheda", "for": "verdi"}, 1, ["FAIL verdi signs as titolare or legale rappresentante: their ruolo is socio"]),
    "scheda-procura": ("with a procura: the procuratore's section from Bianchi (--party procuratore=bianchi)",
                       {"pilot": "scheda", "for": "rossi", "party": ["procuratore=bianchi"], "answers": WITH_PROCURA}, 0,
                       ["ok   bianchi is procuratore", "ok   the form is ready", "wrote: Bianchi", "wrote: Regno Unito",
                        "wrote: Londra", "wrote: Via Chiaia", "wrote: giulia.bianchi@pec.example.it"]),
    "scheda-procura-unnamed": ("a procura, but nobody named for it: asked, naming the vault's procuratore",
                               {"pilot": "scheda", "for": "rossi", "answers": WITH_PROCURA}, 1, ["whose data? --party procuratore=bianchi"]),
    "scheda-wrong-party": ("a --party who doesn't have the ruolo", {"pilot": "scheda", "for": "rossi", "party": ["procuratore=verdi"],
                                                                      "answers": WITH_PROCURA}, 1, ["FAIL verdi is procuratore: their ruolo is socio"]),
    "scheda-allegato": ("rows repeated by ruolo without columns: their page copied once per socio, the signer left out",
                        {"pilot": "scheda", "for": "rossi", "party": ["procuratore=bianchi", "socio=verdi,gialli,rossi"],
                         "answers": WITH_PROCURA, "entry": both(*SOCI_ALLEGATO), "needs_from_entry": True}, 0,
                        ["ok   the form is ready", "wrote: Verdi", "wrote: Gialli", "wrote: Sara", "page 3: "]),
    "scheda-allegato-left-out": ("a socio the vault has but --party didn't name: refused, so nobody is left off",
                                 {"pilot": "scheda", "for": "rossi", "party": ["procuratore=bianchi", "socio=verdi"],
                                  "answers": WITH_PROCURA, "entry": both(*SOCI_ALLEGATO), "needs_from_entry": True}, 1,
                                 ["also gialli (with --party socio=verdi,gialli)"]),
    "scheda-allegato-nobody": ("an allegato nobody is in: its page keeps the other rows on it",
                               {"pilot": "scheda", "for": "rossi", "party": ["procuratore=bianchi"], "answers": WITH_PROCURA,
                                "entry": both(*SOCI_ALLEGATO), "needs_from_entry": True, "fixture": no_soci}, 0,
                               ["ok   the form is ready", "wrote: Via dei Tribunali", "wrote: Londra", "page 2: "]),
    "cila-allegato-fields": ("an allegato copy of a form with fillable fields: each copy's fields and radio buttons its own",
                             {"pilot": "cila", "for": "rossi", "fixture_file": "business-fixture.json", "party": ["socio=verdi,gialli"],
                              "entry": row("Pec", map={"repeat": {"ruolo": "socio", "except": "dichiarante"}, "fields": ["*:identity.last_name"]}),
                              "needs_from_entry": True}, 0,
                             ["ok   the form is ready", "wrote: Verdi", "wrote: Gialli", "page 2: "]),
    "scheda-ruolo-from-answers": ("answers.json can't give someone a ruolo, so they can't sign through it",
                                  {"pilot": "scheda", "for": "nuovo", "fixture": with_fields(["nuovo:identity.last_name", "Nuovo"]),
                                   "values": {"nuovo:ruolo.tipo": "legale_rappresentante"}}, 1,
                                  ["FAIL answers.json value nuovo:ruolo.tipo is no ruolo", "FAIL nuovo signs as"]),
    "scheda-two-procuratori": ("two people named for the procuratore's one section: asked which",
                               {"pilot": "scheda", "for": "rossi", "party": ["procuratore=bianchi,blu"], "answers": WITH_PROCURA,
                                "fixture": with_fields(["blu:ruolo.tipo", "procuratore"], ["blu:identity.last_name", "Blu"])}, 1,
                               ["which one? --party procuratore=bianchi or blu"]),
    "scheda-personal-for-giulia": ("filed for Giulia in a personal vault: the role she signs as is hers, never the owner's",
                                   {"pilot": "scheda", "for": "giulia", "fixture_file": "fixture.json", "fixture": with_fields(*OWNER_BUSINESS)}, 1,
                                   ["giulia:business.qualita_dichiarante not in the vault"]),
    "ricorso-choices-on-concat": ("boxes to tick on a row of several fields: refused, not a crash",
                                  {"pilot": "ricorso", "entry": row("Il sottoscritto", choices=[{"box": "x", "values": ["y"]}])}, 1,
                                  ["choices need a row of one field"]),
    "scheda-personal": ("the scheda in a personal vault: the owner signs as their business.qualita_dichiarante",
                        {"pilot": "scheda", "fixture_file": "fixture.json", "fixture": with_fields(*OWNER_BUSINESS)}, 0,
                        ["ok   the form is ready", "wrote: Ditta Mario Rossi", "wrote: X", "wrote: 7"]),
    "cila-wrong-widget": ("a box names a fillable field the form doesn't have", {"pilot": "cila", "layout": box("C.F.", widget="Text Box 99")}, 1,
                          ["no fillable field 'Text Box 99' on page 1"]),
    "cila-drawn-over-field": ("a box drawn over one of the form's fillable fields instead of naming it",
                              {"pilot": "cila", "layout": lambda l: l["boxes"].__setitem__(1, {"label": "C.F.", "rect": [80, 152, 228, 167]})},
                              1, ["covers the fillable field 'Text Box 1_2'"]),
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


def unit_checks(ricorso_entry):
    """What a case can't reach: an address box after text fill.py itself
    wrote, and needs finding the vault schema through a symlinked skill."""
    import fitz
    bad = []
    doc = fitz.open()
    page = doc.new_page()
    page.insert_text((20, 100), "Indirizzo")
    pen = fill.Pen(fitz, doc, 9)
    pen.put([70, 90, 100, 102], "Via")
    if (got := pen.address(1, [110, 90, 400, 102], "Via Roma 3")) != "Via Roma 3":
        bad.append(f"after our own 'Via', an address became {got!r}")
    with tempfile.TemporaryDirectory() as d:
        os.symlink(os.path.abspath(FILL), os.path.join(d, "fill.py"))
        got = subprocess.run([sys.executable, "-I", os.path.join(d, "fill.py"), "needs", "--entry", ricorso_entry,
                              "--form", "ricorso-prefetto", "--for", "giulia", "--vault-dir", os.path.join(d, "none")],
                             capture_output=True, text=True)
        if got.returncode != 0 or "giulia:identity.*" not in got.stdout:
            bad.append(f"needs through a symlink: {got.stdout[-200:]}{got.stderr[-300:]}")
    return bad


def office_checks(library):
    """office_for's answer when the vault can't give the office field: no
    scope, a street across a boundary, nothing stored; and the office when it
    can."""
    class Stub:
        def __init__(self, status, body):
            self.answer = (status, body)

        def field_path(self, field_id):
            return field_id

        def call(self, method, path):
            return self.answer
    a = types.SimpleNamespace(library=library, server="server.json", vault_dir=None, keychain=None)
    cases = [
        (403, {"error": "scope", "constraint": "scope_exceeded"}, "the token can't read residenza.municipalita"),
        (404, {"error": "street crosses a boundary: Chiaia, San Ferdinando", "constraint": "not_resolved"},
         "Chiaia, San Ferdinando; store the quartiere (pvault set residenza.quartiere"),
        (404, {"error": "field not found", "constraint": "not_found"}, "isn't in the vault"),
        (200, {"value": "2"}, "Sede della Municipalità 2"),
    ]
    bad = []
    connect = fill.connect
    try:
        for status, body, line in cases:
            fill.connect = lambda a, answer=(status, body): {"prepare": Stub(*answer)}
            got = json.dumps(fill.office_for(a, "residenza.municipalita"), ensure_ascii=False)
            if line not in got:
                bad.append(f"{status} {body.get('constraint', '')}: {got}")
    finally:
        fill.connect = connect
    return bad


def ask_checks(scheda_entry):
    """needs' questions for the scheda: the procuratore's rows apply only
    after "Procura conferita?" ticks si, so they are one question, with it."""
    form = next(f for f in scheda_entry["forms"] if f["id"] == "scheda-anagrafica")
    gated = {r["label"] for r in form["fields"] if r.get("when") and "party" in r["map"]}
    asked = fill.asks(form, None)
    bad = [f"asked on its own: {q['label']}" for q in asked if q["label"] in gated]
    gate = next((q for q in asked if q["label"].startswith("3 – Procura conferita")), None)
    if not gated or not gate or "--party procuratore=<slug>" not in gate["question"]:
        bad.append(f"the procura question doesn't ask who the procuratore is: {gate}")
    named = {q["label"]: q for q in fill.asks(form, None, {"procuratore": ["bianchi"]})}
    if gate and "--party" in named[gate["label"]]["question"]:
        bad.append("with --party procuratore named, the procura question still asks who it is")
    return bad


def main():
    library = LIBRARY or sys.exit("set PRATICHE_NAPOLI_DIR to a pratiche-napoli checkout")
    args = sys.argv[1:]
    keep = None
    if args[:1] == ["--keep"]:
        keep, args = args[1], args[2:]
    names = args or list(CASES)
    base = tempfile.mkdtemp()
    pilots = {}

    def pilot(name):
        """The pilot's entry, form, layout and pdf, fetched once."""
        if name not in pilots:
            pid, form = PILOTS[name][:2]
            entry = fill.entry_of(types.SimpleNamespace(entry=None, library=library, procedure=pid, form=form))
            url = next(f["url"] for f in entry["forms"] if f["id"] == form)
            if name == "mod-1" and os.environ.get("PILOT_FORM_PDF"):
                # A copy already on disk, e.g. offline; fill.py still checks its sha256.
                with open(os.environ["PILOT_FORM_PDF"], "rb") as f:
                    pdf = f.read()
            else:
                with urllib.request.urlopen(url, timeout=60) as resp:
                    pdf = resp.read()
            path = os.path.join(base, f"{name}-entry.json")
            with open(path, "w", encoding="utf-8") as f:
                json.dump(entry, f, ensure_ascii=False)
            pilots[name] = {"entry": entry, "entry_path": path, "pdf": pdf,
                            "layout": load(os.path.join(library, "layouts", pid, form + ".json"))}
        return pilots[name]

    try:
        failed = []
        if not args:
            bad = token_commands()
            print(f"{'ok  ' if not bad else 'FAIL'} token-commands: the pvault command needs prints for each vault"
                  + "".join("\n  " + b for b in bad))
            failed += ["token-commands"] if bad else []
            bad = unit_checks(pilot("ricorso")["entry_path"])
            print(f"{'ok  ' if not bad else 'FAIL'} units: an address after our own text; needs through a symlinked skill"
                  + "".join("\n  " + b for b in bad))
            failed += ["units"] if bad else []
            bad = ask_checks(pilot("scheda")["entry"])
            print(f"{'ok  ' if not bad else 'FAIL'} asks: rows that apply only after an answer are asked with it, once"
                  + "".join("\n  " + b for b in bad))
            failed += ["asks"] if bad else []
            bad = office_checks(library)
            print(f"{'ok  ' if not bad else 'FAIL'} offices: the office picked by area, or why not"
                  + "".join("\n  " + b for b in bad))
            failed += ["offices"] if bad else []
        for name in names:
            description, changes, want_exit, want_lines = CASES[name]
            ok, detail = case(base, name, pilot(changes.get("pilot", "mod-1")), changes, want_exit, want_lines)
            print(f"{'ok  ' if ok else 'FAIL'} {name}: {description}{'' if ok else chr(10) + detail}")
            if keep and os.path.isdir(os.path.join(base, name, "out")):
                shutil.copytree(os.path.join(base, name, "out"), os.path.join(keep, name), dirs_exist_ok=True)
            if not ok:
                failed.append(name)
        total = len(names) + (0 if args else 4)
        print(f"{total - len(failed)}/{total} cases passed")
        return 1 if failed else 0
    finally:
        shutil.rmtree(base, ignore_errors=True)


def case(base, name, p, changes, want_exit, want_lines):
    pid, form, answers_file, reply, fixture_file = PILOTS[changes.get("pilot", "mod-1")]
    entry, layout, pdf = p["entry"], p["layout"], p["pdf"]
    signer = (["--for", changes["for"]] if "for" in changes else []) + [x for p in changes.get("party", []) for x in ("--party", p)]
    work = os.path.join(base, name)
    out = os.path.join(work, "out")
    os.makedirs(out)
    # The unchanged entry's tokens: an entry fill.py refuses must be refused
    # by run, which is what a case checks. needs refusing is the case's output.
    entry_path = p["entry_path"]
    if changes.get("needs_from_entry"):
        # Tokens for what the changed entry reads (a deadline, records).
        changed = copy.deepcopy(entry)
        changes["entry"](changed)
        entry_path = os.path.join(work + "-needs.json")
        with open(entry_path, "w", encoding="utf-8") as f:
            json.dump(changed, f, ensure_ascii=False)
    got = subprocess.run([sys.executable, "-I", FILL, "needs", "--entry", entry_path, "--form", form] + signer,
                         capture_output=True, text=True)
    if got.returncode != 0:
        return result(got.returncode, got.stdout + got.stderr, out, want_exit, want_lines)
    needs = got.stdout
    fixture = load(os.path.join(HERE, changes.get("fixture_file", fixture_file)))
    if "fixture" in changes:
        changes["fixture"](fixture)
    entry = copy.deepcopy(entry)
    if "entry" in changes:
        changes["entry"](entry)
    layout = copy.deepcopy(layout)
    if "layout" in changes:
        changes["layout"](layout)
    answers = load(os.path.join(HERE, answers_file))
    answers["answers"] = {} if changes.get("answers") == {} else {**answers["answers"], **changes.get("answers", {})}
    if "values" in changes:
        answers["values"] = changes["values"]
    files = {"fixture.json": fixture, "entry.json": entry, "layout.json": layout, "answers.json": answers}
    for file, data in files.items():
        with open(os.path.join(work, file), "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False)
    if changes.get("sign"):
        # The prepare token also files signing requests, as it is asked for
        # when the layout has a signature box.
        needs = subprocess.run([sys.executable, "-I", FILL, "needs", "--entry", os.path.join(work, "entry.json"),
                                "--layout", os.path.join(work, "layout.json"), "--form", form] + signer,
                               check=True, capture_output=True, text=True).stdout
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
        common = [sys.executable, "-I", FILL, "run", "--library", LIBRARY,
                  "--entry", os.path.join(work, "entry.json"), "--form", form,
                  "--layout", os.path.join(work, "layout.json"), "--pdf", os.path.join(work, "form.pdf"), "--out", out,
                  "--answers", os.path.join(work, "answers.json")] + signer
        if changes.get("library"):
            # The form a past run left in the output folder is used, not fetched again.
            shutil.copy(os.path.join(work, "form.pdf"), os.path.join(out, "form.pdf"))
            common = [sys.executable, "-I", FILL, "run", "--library", LIBRARY,
                      "--procedure", pid, "--form", form, "--out", out, "--answers", os.path.join(work, "answers.json")] + signer
        if "route" in changes:
            source = (["--entry", os.path.join(work, "entry.json")] if changes.get("entry_only")
                      else ["--library", LIBRARY, "--procedure", pid])
            run = subprocess.run([sys.executable, "-I", FILL, "route", *source, "--channel", changes["route"],
                                  "--server", os.path.join(work, "server.json")], capture_output=True, text=True)
        elif changes.get("socket"):
            # As on a real vault: its folder, the token from the environment, a draft.
            if "chmod_run" in changes:
                os.chmod(os.path.join(server["vault_dir"], "run"), changes["chmod_run"])
            run = subprocess.run(common + ["--vault-dir", server["vault_dir"], "--draft"], capture_output=True, text=True,
                                 env={**os.environ, "VAULT_TOKEN": server["tokens"]["prepare"]})
            run.stdout += written_pratiche(server)
        else:
            if "reply" in changes:
                reply = os.path.join(work, "reply.json")
                with open(reply, "w", encoding="utf-8") as f:
                    json.dump(changes["reply"], f)
            played = ["--reply", os.path.join(HERE, reply)] if reply and not changes.get("no_reply") else []
            args = ["--server", os.path.join(work, "server.json")] + played
            args += ["--sign"] if changes.get("sign") else []
            args += ["--channel", changes["channel"]] if changes.get("channel") else []
            run = subprocess.run(common + args, capture_output=True, text=True)
            if changes.get("again"):
                second = subprocess.run(common + args, capture_output=True, text=True)
                run.stdout += "\n--- second run ---\n" + second.stdout
                run.stderr += second.stderr
                run.returncode = run.returncode or second.returncode
            if changes.get("sign") and not changes.get("no_reply") and run.returncode == 0:
                run.stdout += review_signing(server, out, layout)
    finally:
        open(os.path.join(work, "done"), "w").close()
        harness.wait()
        log.close()
    return result(run.returncode, run.stdout + run.stderr, out, want_exit, want_lines)


def result(code, output, out, want_exit, want_lines):
    """The case's verdict: its exit code, and each wanted line in the output
    or among the values the report says were written."""
    try:
        report = load(os.path.join(out, "report.json"))
        output += "".join(f"\nwrote: {r['expected']}" for r in report["read_back"])
        output += "".join(f"\npage {n}: " for n in sorted({r.get("page", 1) for r in report["read_back"]}))
    except FileNotFoundError:
        pass
    lacking = [line for line in want_lines if line not in output]
    if "Traceback (most recent call last)" in output:
        lacking.append("no traceback")
    if code != want_exit or lacking:
        return False, f"  exit {code} (want {want_exit}); lacking {lacking}\n" + "\n".join("  | " + l for l in output.splitlines()[-25:])
    return True, ""


if __name__ == "__main__":
    sys.exit(main())
