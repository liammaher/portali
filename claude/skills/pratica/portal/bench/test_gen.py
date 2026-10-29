#!/usr/bin/env python3
"""gen.py offline, on the crawls in catalogue/F839/sections: no browser, no network.

  ~/.cache/pratica-portal/venv/bin/python claude/skills/pratica/portal/bench/test_gen.py
"""

import copy
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[4]
sys.path.insert(0, str(HERE.parent))
sys.dont_write_bytecode = True
import gen  # noqa: E402

CILA = ROOT / "rehearsals" / "cila-fittizia"
FINE = ROOT / "rehearsals" / "fine-lavori-fittizia"
results = []


def case(name):
    def run(fn):
        try:
            fn()
            results.append(True)
            print(f"ok    {name}")
        except Exception as e:  # noqa: BLE001
            results.append(False)
            print(f"FAIL  {name}: {e}")
    return run


# The hand walk was P6's case: a tenant (altro + locatario, the owner company's assent) and
# works under way (d.2). The dictionary now maps every title and stage under `when`; these
# entries are P6's alone, as the dictionary had them, so the id arithmetic is still checked.
P6_ONLY = gen.yaml.safe_load("""
- section: a) Titolarità dell'intervento
  fields:
    - {kind: select, label: "Il richiedente dichiara di avere titolo", value: altro}
    - {kind: text, label: specificare, value: locatario}
    - {kind: radio, label: non avere titolarità esclusiva}
    - {kind: text, label: Cognome, value: "{{r:identity.last_name}}"}
    - {kind: text, label: Nome, value: "{{r:identity.first_name}}"}
    - {kind: text, label: C.F., value: "{{r:identity.codice_fiscale}}"}
    - {kind: date, label: Data di nascita, value: "{{r:identity.date_of_birth|date_it}}"}
    - {kind: select, label: Sesso, value: "{{r:identity.sex|sesso}}"}
    - {kind: select, label: Provincia, value: "{{r:identity.birth_province}}"}
    - {kind: text, label: Comune italiano, value: "{{r:identity.birth_comune|upper}}"}
    - {kind: select, label: Provincia, nth: 2, value: "{{r:residenza.provincia}}"}
    - {kind: text, label: Comune italiano, nth: 2, value: "{{r:residenza.comune|upper}}"}
    - {kind: text, label: "Via, Piazza", value: "{{r:residenza.indirizzo|upper}}"}
    - {kind: text, label: "N.", value: "{{r:residenza.civico}}"}
    - {kind: text, label: C.A.P., value: "{{r:residenza.cap}}"}
    - {kind: text, label: PEC / posta elettronica, value: "{{o:business.pec}}"}
    - {kind: text, label: Telefono, value: "{{o:business.telefono|phone_it}}"}
    - {kind: checkbox, label: Da compilare solo nel caso in cui il titolare sia}
    - {kind: text, label: in qualità di, value: legale rappresentante}
    - {kind: text, label: della ditta / società, value: "{{o:business.denominazione}}"}
    - {kind: text, label: con codice fiscale / p. IVA, value: "{{o:business.partita_iva}}"}
- section: d) Qualificazione dell'intervento
  fields:
    - {kind: radio, label: intervento in corso di esecuzione}
    - {kind: date, label: intervento iniziato in data, value: "{{ask.data_inizio_lavori|date_it}}"}
    - {kind: file, label: "Tipologie ammesse", value: "{{ask.ricevuta_sanzione}}"}
- section: e) Destinazione d'uso
  fields:
    - {kind: select, label: "L'intervento interessa l'immobile", value: "{{c:immobile.destinazione_uso}}"}
""")


def p6_recipe():
    return gen.load_yaml(CILA / "recipe.yaml") | {"sections": P6_ONLY}


@case("the CILA comes out as the hand walk found it, field for field (P6's own entries)")
def _():
    diffs, _ = gen.check(gen.generate(p6_recipe()), gen.load_yaml(CILA / "portal-steps.yaml"))
    assert not diffs, diffs


@case("two choices the crawl tried apart add up to the hand walk's ids (a), owner rows)")
def _():
    steps = gen.generate(p6_recipe())
    a = next(s for s in steps["sections"] if s["section"].startswith("a)"))
    ids = {f["label"]: f["id"] for f in a["fields"]}
    assert ids["Cognome"] == "r11c1p13reg:txt" and ids["con codice fiscale / p. IVA"] == "r32c2p77reg:txt", ids


def p6_values(name, answers="answers-p6.json"):
    import json
    import run
    fields = dict(json.loads((CILA / "fixture-p6.json").read_text())["fields"])
    return lambda steps: run.Values(json.loads((ROOT / "rehearsals" / name / answers).read_text()),
                                    fields=fields, asks=steps.get("asks"))


def filled(steps, values):
    """Each section's values in order, as run.py's plan fills them for these answers."""
    import os
    import run
    here = os.getcwd()
    os.chdir(ROOT)  # answers name files from the repo's root
    try:
        out, v = {}, values(steps)
        run.plan(steps, v)  # stops on a missing value
        for sec in steps["sections"]:
            for f in sec["fields"]:
                # A box only some pratiche show is skipped where it isn't, so it isn't compared.
                if f.get("on_page") or f.get("if_present") or "when" in f and not run.truthy(v.render(f["when"]) or ""):
                    continue
                out.setdefault(gen.norm(gen.QUADRO.sub("", sec["section"])), []).append(
                    v.render(f["value"]) if "value" in f else True)
        return out
    finally:
        os.chdir(here)


@case("the dictionary's every-case CILA fills P6 with the hand walk's values, section for section")
def _():
    v = p6_values("cila-fittizia")
    hand, ours = filled(gen.load_yaml(CILA / "portal-steps.yaml"), v), filled(gen.generate(gen.load_yaml(CILA / "recipe.yaml")), v)
    for sec, vals in hand.items():
        assert ours.get(sec) == vals, (sec, vals, ours.get(sec))


@case("the fine lavori, never walked, comes out whole from the shared sections")
def _():
    steps = gen.generate(gen.load_yaml(FINE / "recipe.yaml"))
    names = [s["section"] for s in steps["sections"]]
    assert "ubicazione dell'immobile" in names and "comunicazione di fine lavori" in names, names
    slots = next(s for s in steps["sections"] if s["section"] == "Gestione degli allegati")["fields"]
    assert [f["label"][:20] for f in slots] == ["Fotocopia di un docu", "Copia dei documenti ", "Dichiarazione di ass"], slots


@case("a section the dictionary doesn't have stops it, nothing written")
def _():
    real = gen.load_yaml
    def without(p):
        d = real(p)
        if p.name == "fnsuap.yaml":
            d = copy.deepcopy(d)
            d["sections"] = [s for s in d["sections"] if s["section"] != "comunicazione di fine lavori"]
        return d
    gen.load_yaml = without
    try:
        gen.generate(real(FINE / "recipe.yaml"))
        raise AssertionError("generated anyway")
    except gen.GenError as e:
        assert "comunicazione di fine lavori: no dictionary entry" in str(e), e
    finally:
        gen.load_yaml = real


@case("an attachments screen nobody has crawled stops it")
def _():
    recipe = gen.load_yaml(FINE / "recipe.yaml")
    real = gen.json.loads
    gen.json.loads = lambda s: {k: v for k, v in real(s).items() if k != "slots"} if isinstance(real(s), dict) else real(s)
    try:
        gen.generate(recipe)
        raise AssertionError("generated anyway")
    except gen.GenError as e:
        assert "slots aren't crawled yet" in str(e), e
    finally:
        gen.json.loads = real


REH = ROOT / "rehearsals"


def generated(name):
    return gen.generate(gen.load_yaml(REH / name / "recipe.yaml"))


def section(steps, start):
    return next(s for s in steps["sections"] if s["section"].startswith(start))


def field(sec, start):
    return next(f for f in sec["fields"] if f.get("label", "").startswith(start))


@case("a quadro is matched without its letter, and a tick is a radio or a checkbox as the page has it (CIL)")
def _():
    steps = generated("cil-fittizia")
    g = section(steps, "g) Rispetto")  # the CILA's i)
    assert field(g, "dichiara che l'entità presunta")["kind"] == "checkbox"  # a radio in the CILA
    f = section(steps, "f) Impresa")["fields"]
    assert next(x for x in f if "anchor" in x)["anchor"] == "Ragione sociale"


@case("two exclusive branches keep their own crawled ids (agibilità, fine lavori filed or not)")
def _():
    s = section(generated("agibilita-fittizia"), "dichiarazione sulla comunicazione")
    assert field(s, "prot./n.")["id"] == "r4c1p6reg:txt"
    assert "id" not in field(s, "che la presente segnalazione")  # after the other branch's rows: by label


@case("a field below a conditional choice that shifts rows goes by label; one above keeps its id (SCIA impianti)")
def _():
    s = section(generated("scia-fittizia"), "Asseverazione SCIA - Sicurezza impianti")
    assert field(s, "idrici e sanitari")["id"] == "r9c1p9reg:chk"
    assert "id" not in field(s, "non è soggetto agli obblighi")


@case("rerun.py queues each rehearsal with run.js's own values, and check.js names each steps file")
def _():
    import json, subprocess, tempfile
    fixture = json.loads((REH / "cila-fittizia" / "fixture-p6.json").read_text())
    with tempfile.TemporaryDirectory() as t:
        t = Path(t)
        (t / "values.json").write_text(json.dumps(dict(fixture["fields"])))
        names = ["dau-fittizia", "paesaggistica-semplificata-fittizia"]
        subprocess.run([sys.executable, str(HERE / "rerun.py"), "--values", str(t / "values.json"), "--out", str(t / "out"), *names],
                       check=True, capture_output=True)
        queue = (t / "out" / "queue-0.js").read_text()
        check = (t / "out" / "check.js").read_text()
        assert all(f'"steps/{n}.js"' in check for n in names) and '"runner.js"' in check
        assert '["dau-fittizia","napoli/comune/dichiarazione-avvenuto-utilizzo-terre",{"wizard":["PROVA NON INVIARE - DAU P6 generato"' in queue
        assert (t / "out" / "steps" / "dau-fittizia.js").read_text().startswith('localStorage.setItem("pv-steps:napoli/comune/dichiarazione-avvenuto-utilizzo-terre"')


@case("two operations in one pratica: their sections once each, in the combination's crawl's order, and both ticked")
def _():
    steps = generated("cila-paesaggistica-fittizia")
    names = [x["section"] for x in steps["sections"]]
    assert len(names) == len(set(n.lower() for n in names)) == 22
    assert names.index("a) Titolarità dell'intervento") < names.index("Qualificazione dell'intervento") < names.index("Ambito dell'intervento")
    assert "dichiarazione ubicazione dell'intervento" not in names  # the CILA's Localizzazione, the same section
    assert steps["wizard"][3]["tree"] == ["Comunicazione Inizio Lavoro Asseverata (C.I.L.A.)",
                                          "Autorizzazione paesaggistica semplificata (DPR 31/2017 - Comune)"]
    recipe = gen.load_yaml(REH / "cila-paesaggistica-fittizia" / "recipe.yaml")
    recipe["crawl"] = "cila"  # a crawl of the CILA alone is not of both
    try:
        gen.generate(recipe)
        assert False, "generated from the wrong crawl"
    except gen.GenError as e:
        assert "isn't a crawl of these operations together" in str(e)


@case("two scelte both keep the sections they add, and the wizard ticks both (voltura with a co-holder)")
def _():
    steps = generated("voltura-pdc-fittizia")
    names = [x["section"] for x in steps["sections"]]
    assert "Informazioni su eventuali altri aventi titolo" in names and "Qualifica e dati del procuratore" in names
    assert steps["wizard"][-1]["tree"][0] == "ci sono cointestatari" and len(steps["wizard"][-1]["tree"]) == 2


@case("a section name two pratiche share takes the entry that fits; a tick that renumbers itself and what follows go by label (paesaggistica semplificata)")
def _():
    steps = generated("paesaggistica-semplificata-fittizia")
    q = section(steps, "Qualificazione dell'intervento")  # the SCIA's has one radio
    assert len(q["fields"]) == 42 and field(q, "B.10.")["id"] == "r12c1p13reg:chk"
    a = section(steps, "Ambito dell'intervento")
    both = [f for f in a["fields"] if f["when"] == "{{ask.vincolo|is_art_136_and_142}}"]
    # Ticking art. 136 opens its rows above itself: r2c1p2 becomes r3c1p3 (run of 2026-10-29).
    ticks = [f for f in a["fields"] if f["kind"] == "checkbox" and f["label"] == "dell'art. 136"]
    assert len(ticks) == 2 and all("id" not in f for f in ticks)
    assert all("id" not in f for f in both[1:] if f["label"] != "dell'art. 136")
    # A pair of same-label boxes crawl.js matched isn't a move (the SCIA's tutela radio).
    scia = generated("scia-fittizia")
    assert any(f.get("id") == "r10c1p10reg:radio:3_clone" for s in scia["sections"] for f in s["fields"])
    assert "allegato_b" in steps["groups"]


@case("a slot that takes only signed files is the person's, never a plain copy (SCIA stato legittimo)")
def _():
    s = section(generated("scia-fittizia"), "Gestione degli allegati")
    assert "Documentazione dimostrativa dello stato legittimo" in s["theirs"]
    assert not any(f["label"].startswith("Documentazione dimostrativa") for f in s["fields"])


@case("a question only a missing field reads isn't asked (SCIA has no firm-documents check)")
def _():
    assert "verificato_idoneita" not in generated("scia-fittizia")["asks"]


@case("draft.py drafts the hairdresser's own sections from the national schema, every box with its meaning")
def _():
    import draft
    crawl = gen.json.loads((HERE.parent / "catalogue/F839/sections/acconciatore.json").read_text())
    schema = gen.json.loads((HERE.parent / "schema/mod_acconciatori_estetisti_apertura_v1.0.7-fields.json").read_text())
    sec = lambda name: next(x for x in crawl["sections"] if x["section"].startswith(name))
    e, asks, n, missed = draft.draft_section(sec("Dati sull"), schema)
    got = [f.get("value") or f.get("when") for f in e["fields"]]
    assert missed == 0 and got[:2] == ["{{ask.superficie_complessiva}}", "{{ask.superficie_attivita}}"], got
    assert "{{ask.congiunta_denominazione}}" in got and "{{ask.data_protocollo|date_it}}" in got, got
    e, asks, n, missed = draft.draft_section(sec("Dichiarazione sul possesso dei requisiti antimafia"), schema)
    assert e.get("theirs") and not e["fields"], e  # a declaration is the person's
    e, asks, n, missed = draft.draft_section(sec("Dichiarazione sul possesso dei requisiti professionali"), schema)
    by = {(f["label"][:12], f.get("nth", 1)): f.get("value") for f in e["fields"] if f.get("value")}
    assert by[("rilasciata d", 1)] == "{{ask.ente_abilitazione_professionale}}", by
    assert by[("rilasciata d", 2)] == "{{ask.ente_qualifica_professionale}}", by
    assert by[("Nome", 1)] == "{{ask.nome}}", by


def batch_copy(tmp):
    """A copy of the catalogue where a batch crawled the fine lavori again: every section it
    had seen before named by reference (`same_as`), as crawl.js's batch writes them."""
    import shutil
    base = Path(tmp)
    shutil.copytree(HERE.parent / "catalogue", base / "catalogue")
    shutil.copytree(HERE.parent / "dictionary", base / "dictionary")
    fine = gen.json.loads((base / "catalogue/F839/sections/fine-lavori.json").read_text())
    for s in fine["sections"]:
        if s.get("fields") is not None:
            for k in [k for k in s if k != "section"]:
                del s[k]
            s["same_as"] = f"fine-lavori#{s['section']}"
    (base / "catalogue/F839/sections/fine-batch.json").write_text(gen.json.dumps(fine))
    return base


@case("a section a batch crawl names by reference (same_as) generates as the section itself")
def _():
    import tempfile
    with tempfile.TemporaryDirectory() as tmp:
        base = batch_copy(tmp)
        recipe = gen.load_yaml(FINE / "recipe.yaml")
        want = gen.generate(recipe)
        got = gen.generate(dict(recipe, crawl="fine-batch"), base=base)
        want.pop("generated"), got.pop("generated")
        assert got == want


@case("a section's fingerprint is the one crawl.js computed in the page, for every crawled section")
def _():
    import catalogue
    n = 0
    for name, c in catalogue.crawls().items():
        for s in c["sections"]:
            if s.get("fields") is not None and s.get("fingerprint"):
                assert catalogue.fingerprint(s["fields"]) == s["fingerprint"], (name, s["section"])
                n += 1
    assert n > 100, n


@case("a pratica the last drift check found changed is refused by gen.py and by pre-flight")
def _():
    import tempfile
    with tempfile.TemporaryDirectory() as tmp:
        base = batch_copy(tmp)
        (base / "catalogue/F839/drift.json").write_text(gen.json.dumps(
            {"pratiche": {"fine-lavori": {"checked": "2026-10-28", "differences": ["changed: Dati catastali"]}}}))
        try:
            gen.generate(gen.load_yaml(FINE / "recipe.yaml"), base=base)
            raise AssertionError("generated")
        except gen.GenError as e:
            assert "changed: Dati catastali" in str(e), e
        import catalogue
        old = catalogue.CAT
        catalogue.CAT = base / "catalogue/F839"
        try:
            import run
            steps = gen.load_yaml(FINE / "portal-steps.generated.yaml")
            assert "Dati catastali" in run.portal_changed(steps)[0]
            assert run.portal_changed(gen.load_yaml(CILA / "portal-steps.yaml")) == []  # hand-written
        finally:
            catalogue.CAT = old


@case("draft.py drafts a restaurant's opening from two national forms (its SCIA and the Reg. 852 notifica)")
def _():
    import draft
    crawl = gen.json.loads((HERE.parent / "catalogue/F839/sections/suap-060-aprire-l-attivita.json").read_text())
    schema = [x for f in ("mod_somministrazione_zone_non_tutelate_apertura", "mod_notifica_sanitaria")
              for x in gen.json.loads((HERE.parent / f"schema/{f}_v1.0.7-fields.json").read_text())]
    sec = lambda name: next(x for x in crawl["sections"] if x["section"].startswith(name))
    leaf = lambda asks, v: asks[gen.re.findall(r"ask\.(\w+)", v)[-1]]["schema"].rsplit("/", 1)[-1]
    e, asks, n, missed = draft.draft_section(sec("Dati dell'esercizio"), schema)
    got = {f["label"]: leaf(asks, f.get("value") or f["when"]) for f in e["fields"]}
    assert got["mq"] == "superficie" and got["Tipologia di esercizio"] == "tipologia_esercizio", got  # not the form's own word
    assert got["dal (gg/mm)"] == "da_stagionale", got
    e, asks, n, missed = draft.draft_section(sec("tipologia di attivit"), schema)
    assert e["schema"].endswith("avvio_attivita_section"), e["schema"]  # the second form's group
    got = lambda start: next(leaf(asks, f["when"]) for f in e["fields"] if f["label"].startswith(start))
    assert got("COMMERCIO AL DETTAGLIO") == "commercio_dettaglio"
    assert got("Carne, prodotti") == "carne"  # the rare word, not "prodotti"
    assert got("Ristorazione pubblica con") == "ristorazione_pubblica"
    e, asks, n, missed = draft.draft_section(sec("DICHIARAZIONE ESENZIONE"), schema)
    assert e.get("theirs") and not e["fields"], e  # "Presa visione": the person's


@case("draft.py pairs by order and type: the portal's address block on the form's one-line address, the previous site's side")
def _():
    import draft
    crawl = gen.json.loads((HERE.parent / "catalogue/F839/sections/suap-060-trasferire-la-sede-dell-attivita.json").read_text())
    schema = gen.json.loads((HERE.parent / "schema/mod_somministrazione_zone_non_tutelate_trasferimento_v1.0.7-fields.json").read_text())
    e, asks, n, missed = draft.draft_section(next(x for x in crawl["sections"] if x["section"].startswith("precedente")), schema)
    own = [f["label"] for f in e["fields"] if f.get("note")]
    assert "Frazione" in own and "Via, Viale, Piazza" not in own, own  # the address block: its Via is the line
    leaf = lambda f: asks[gen.re.findall(r"ask\.(\w+)", f.get("value") or f["when"])[-1]]["schema"].rsplit("/", 1)[-1]
    got = {f["label"]: leaf(f) for f in e["fields"] if not f.get("note")}
    assert got["Via, Viale, Piazza"] == "indirizzo_da" and got["CAP"] == "cap_da", got  # "precedente": the _da side
    assert got["Si"] == "centro_commerciale_si_da", got


@case("the dictionary names each question once (YAML keeps the last of two quietly)")
def _():
    seen = []

    class Strict(gen.yaml.SafeLoader):
        def construct_mapping(self, node, deep=False):
            keys = [self.construct_object(k, deep=deep) for k, _ in node.value]
            seen.extend(k for k in set(keys) if keys.count(k) > 1)
            return super().construct_mapping(node, deep)
    gen.yaml.load((HERE.parent / "dictionary/fnsuap.yaml").read_text(), Loader=Strict)
    assert not seen, seen


@case("a choice's answer picks its branch (is_<answer>), and an ordine's name its albo option")
def _():
    import run
    assert run.FILTERS["is_permesso_di_costruire"]("Permesso di costruire") == "true"
    assert run.FILTERS["is_scia"]("permesso di costruire") == "false"
    assert run.FILTERS["albo_tipo"]("Ordine degli Architetti P.P.C.") == "Architetto"


@case("an operation checked in another sector is that pratica when every fingerprint matches, else one to crawl")
def _():
    import tempfile, catalogue
    with tempfile.TemporaryDirectory() as tmp:
        base = batch_copy(tmp)
        old = (catalogue.CAT, catalogue.SECTIONS, catalogue.SAME)
        catalogue.CAT = base / "catalogue/F839"
        catalogue.SECTIONS, catalogue.SAME = catalogue.CAT / "sections", catalogue.CAT / "same.json"
        try:
            fps = catalogue.stored_fingerprints("fine-lavori")
            check = lambda name, change: {"name": name, "like": "fine-lavori", "crawled": "2026-10-28T00:00:00Z", "sections":
                [{"section": k, "fingerprint": ("0" * 16 if k == change else v)} for k, v in fps.items()]}
            export = base / "export.json"
            export.write_text(gen.json.dumps({"crawl:check:suap-001-x": check("suap-001-x", None),
                                              "crawl:check:suap-002-x": check("suap-002-x", "Oggetto della pratica")}))
            out = catalogue.cmd_drift(type("a", (), {"export": str(export)}))
            same = gen.json.loads(catalogue.SAME.read_text())
            assert same["same"] == {"suap-001-x": "fine-lavori"}, out
            assert same["different"]["suap-002-x"]["differences"] == ["changed: Oggetto della pratica"], out
            assert not (catalogue.CAT / "drift.json").exists() or "suap-001-x" not in (catalogue.CAT / "drift.json").read_text()
        finally:
            catalogue.CAT, catalogue.SECTIONS, catalogue.SAME = old


@case("a choice made for only some answers inside a branch sends what follows by label (CILA tolerances, d.1)")
def _():
    steps = gen.generate(gen.load_yaml(CILA / "recipe.yaml"))
    rel = section(steps, "Relazione Tecnica")
    assert field(rel, "2%, per le U.I.")["id"]  # opened by the 1-bis tolerance: its own state
    assert "id" not in field(rel, "TOLLERANZA DI CUI ALL’ART. 34-BIS, COMMA 2:")  # after 1-bis's rows, ticked or not
    d = section(steps, "d) Qualificazione")
    assert field(d, "data inizio lavori")["id"] and "id" not in field(d, "i lavori avranno inizio dopo")


@case("a firm's block is filled only when the job has that firm, and none when the client works in proprio")
def _():
    steps = gen.generate(gen.load_yaml(CILA / "recipe.yaml"))
    p6 = [v for v in filled(steps, p6_values("cila-fittizia"))[gen.norm("Impresa esecutrice dei lavori")] if v]
    owner = filled(steps, p6_values("cila-fittizia", "answers-proprietario.json"))[gen.norm("Impresa esecutrice dei lavori")]
    alone = filled(steps, p6_values("cila-fittizia", "answers-comodatario.json"))[gen.norm("Impresa esecutrice dei lavori")]
    assert "Impianti Fittizi" in str(p6) and "Impianti" not in str(owner) and "Edil" in str(owner), owner
    assert alone == [True], alone  # just "in proprio"


@case("a SCIA on structures fills the structural designer's block by label, and a SCIA condizionata gives no start date")
def _():
    steps = gen.generate(gen.load_yaml(ROOT / "rehearsals" / "scia-fittizia" / "recipe.yaml"))
    h1 = section(steps, "h1) Tecnici")
    block = [f for f in h1["fields"] if f.get("anchor") == "Cognome"]
    assert block and all("id" not in f for f in block)  # its labels repeat the architect's
    structural = filled(steps, p6_values("scia-fittizia", "answers-strutture.json"))
    plain = filled(steps, p6_values("scia-fittizia"))
    assert "SMPDVD80E20F839S" in structural[gen.norm("Tecnici incaricati")]
    assert "SMPDVD80E20F839S" not in plain[gen.norm("Tecnici incaricati")]
    b = gen.norm("Presentazione della SCIA/SCIA Unica/SCIA Condizionata")
    assert structural[b] == ["23/11/2026"], structural[b]
    assert filled(steps, p6_values("scia-fittizia", "answers-tutele.json")).get(b, []) == []  # assents asked with it


print(f"{sum(results)}/{len(results)} passed")
sys.exit(0 if all(results) else 1)
