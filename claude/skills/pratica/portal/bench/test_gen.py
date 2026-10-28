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


print(f"{sum(results)}/{len(results)} passed")
sys.exit(0 if all(results) else 1)
