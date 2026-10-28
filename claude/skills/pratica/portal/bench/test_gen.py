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


@case("the CILA comes out as the hand walk found it, field for field")
def _():
    diffs, _ = gen.check(gen.generate(gen.load_yaml(CILA / "recipe.yaml")), gen.load_yaml(CILA / "portal-steps.yaml"))
    assert not diffs, diffs


@case("two choices the crawl tried apart add up to the hand walk's ids (a), owner rows)")
def _():
    steps = gen.generate(gen.load_yaml(CILA / "recipe.yaml"))
    a = next(s for s in steps["sections"] if s["section"].startswith("a)"))
    ids = {f["label"]: f["id"] for f in a["fields"]}
    assert ids["Cognome"] == "r11c1p13reg:txt" and ids["con codice fiscale / p. IVA"] == "r32c2p77reg:txt", ids


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
    assert section(steps, "f) Impresa")["fields"][1]["anchor"] == "Ragione sociale"


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


@case("draft.py leaves a box blank rather than pair it on kind alone (a previous address the form keeps as one line)")
def _():
    import draft
    crawl = gen.json.loads((HERE.parent / "catalogue/F839/sections/suap-060-trasferire-la-sede-dell-attivita.json").read_text())
    schema = gen.json.loads((HERE.parent / "schema/mod_somministrazione_zone_non_tutelate_trasferimento_v1.0.7-fields.json").read_text())
    e, asks, n, missed = draft.draft_section(next(x for x in crawl["sections"] if x["section"].startswith("precedente")), schema)
    blank = [f["label"] for f in e["fields"] if f.get("note")]
    assert "Via, Viale, Piazza" in blank and "Frazione" in blank, blank


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


print(f"{sum(results)}/{len(results)} passed")
sys.exit(0 if all(results) else 1)
