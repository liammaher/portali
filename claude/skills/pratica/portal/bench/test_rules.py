#!/usr/bin/env python3
"""Offline tests of the rules tables (rules.py) and how gen.py and run.py preflight use them.

  ~/.cache/pratica-portal/venv/bin/python -I claude/skills/pratica/portal/bench/test_rules.py

No browser, vault or network: the non-food shop's and the restaurant's rehearsals, and the
SUE's building ones (rules/sue-napoli.yaml), changed one fact at a time. With PRATICHE_NAPOLI_DIR set, rules/ is also compared with the library's.
"""

import copy
import itertools
import json
import os
import sys
from pathlib import Path

import yaml

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
sys.dont_write_bytecode = True  # no __pycache__ in the skill's folder
import gen  # noqa: E402
import rules  # noqa: E402
import run  # noqa: E402

REHEARSALS = HERE.parents[4] / "rehearsals"
TABLE = rules.table("suap-napoli")


def case(name, fixture, **answers):
    """A rehearsal's pre-flight report, its answers changed (None drops one)."""
    d = REHEARSALS / name
    steps = yaml.safe_load((d / "portal-steps.generated.yaml").read_text())
    fields = dict(json.loads((REHEARSALS / fixture / "fixture.json").read_text())["fields"])
    a = json.loads((d / "answers.json").read_text())
    for k, v in answers.items():
        if v is None:
            a.pop(k, None)
        else:
            a[k] = v
    return run.preflight(steps, run.Values(a, fields=fields, asks=steps.get("asks")))


def shop(**answers):
    return case("negozio-fittizio", "negozio-fittizio", **answers)


def restaurant(**answers):
    return case("ristorante-fittizio", "pizzeria-fittizia", **answers)


def every_case_decided_once():
    """As the library's validator: every combination of the facts (numbers at each bound
    and past it) is decided by a main rule or refused, never by two that disagree."""
    domains = []
    for name, f in TABLE["facts"].items():
        if f["type"] == "choice":
            domains.append(list(f["values"]))
        elif f["type"] == "yes_no":
            domains.append([True, False])
        else:
            bounds = {b for r in TABLE["rules"] if isinstance(r["when"].get(name), dict) for b in r["when"][name].values()}
            domains.append(sorted({0.0} | bounds | {b + 1 for b in bounds}))
    n = 0
    for combo in itertools.product(*domains):
        d = rules.decide(TABLE, dict(zip(TABLE["facts"], combo)))
        assert not d["conflict"] and (d["main"] or d["refused"]), (combo, d)
        n += 1
    assert n > 10000, n


def decide_answers():
    d = rules.decide(TABLE, {"attivita": "somministrazione", "evento": "apertura"})
    assert d["ask"] == ["zona", "somministrazione_particolare", "prevenzione_incendi", "alcolici", "insegna", "musica",
                        "dehors"], d["ask"]
    assert d["main"] is None
    facts = {"attivita": "somministrazione", "evento": "apertura", "zona": "a", "somministrazione_particolare": False,
             "prevenzione_incendi": False, "alcolici": False, "insegna": False, "musica": True, "dehors": False}
    d = rules.decide(TABLE, facts)
    assert d["main"]["regime"] == "autorizzazione", d
    assert [a["procedure"] for a in d["also"]] == ["napoli/comune/nulla-osta-impatto-acustico",
                                                   "napoli/asl-napoli-1/registrazione-attivita-alimentare"], d["also"]
    d = rules.decide(TABLE, {**facts, "evento": "subingresso"})
    assert d["refused"] and not d["main"], d


def ready_as_proven():
    for name, fx in [("negozio-fittizio", "negozio-fittizio"), ("negozio-insegna-fittizio", "negozio-fittizio"),
                     ("ristorante-fittizio", "pizzeria-fittizia"), ("subingresso-negozio-fittizio", "negozio-fittizio"),
                     ("subingresso-ristorante-fittizio", "pizzeria-fittizia"), ("ampliamento-negozio-fittizio", "negozio-fittizio"),
                     ("cessazione-fittizia", "pizzeria-fittizia"),
                     ("ristorante-centro-storico-fittizio", "ristorante-centro-storico-fittizio")]:
        r = case(name, fx)
        assert r["ready"], (name, r)


def facts_asked():
    r = restaurant(zona=None, fatti=None)
    assert [q["key"] for q in r["questions"]][:2] == ["zona", "fatti"], r["questions"]
    zona = r["questions"][0]
    assert zona["type"] == "choice" and zona["options"] == ["a", "b", "c"] and zona["labels"]["c"] and zona["how"], zona
    items = [i["key"] for i in r["questions"][1]["items"]]
    assert items == ["somministrazione_particolare", "prevenzione_incendi", "alcolici", "musica", "dehors"], items
    # The shop's facts: never its zone, which no rule for a non-food shop reads.
    r = shop(fatti=None)
    assert [q["key"] for q in r["questions"]] == ["fatti"], r["questions"]
    d = REHEARSALS / "negozio-fittizio"
    steps = yaml.safe_load((d / "portal-steps.generated.yaml").read_text())
    answers = {k: v for k, v in json.loads((d / "answers.json").read_text()).items() if k != "fatti"}
    fields = dict(json.loads((d / "fixture.json").read_text())["fields"])
    raises(lambda: run.require_supported(steps, run.Values(answers, fields=fields, asks=steps["asks"])),
           "unanswered: ask.alcolici, ask.dehors, ask.insegna, ask.prevenzione_incendi")


def refused_by_the_table():
    assert shop(superficie_vendita_mq="300")["unsupported"] == [
        next(r["then"]["refuse"] for r in TABLE["rules"] if r["id"] == "vicinato-oltre-250-mq")]
    assert "Agenzia delle Dogane" in shop(fatti=["alcolici"])["unsupported"][0]
    assert restaurant(zona="a")["unsupported"][0].startswith("This case is another pratica: "
                                                             "napoli/comune/scia-somministrazione-alimenti-bevande (autorizzazione) at "
                                                             "Esercizio di somministrazione di alimenti e bevande in zone tutelate")


def other_pratiche():
    assert shop(fatti=["insegna"])["unsupported"] == [
        "This case also needs napoli/comune/insegna-esercizio (scia), which this pratica doesn't file."]
    assert case("negozio-insegna-fittizio", "negozio-fittizio", fatti=[])["unsupported"] == [
        "This pratica also files napoli/comune/insegna-esercizio, which this case doesn't need."]
    assert "concessione-suolo-pubblico-dehors" in restaurant(fatti=["dehors"])["unsupported"][0]


SUE = rules.table("sue-napoli")


def sue(name, answers="answers-p6.json", **changes):
    """A SUE rehearsal's pre-flight report with the P6 fixture's fields, its answers changed
    (None drops one)."""
    d = REHEARSALS / name
    steps = yaml.safe_load((d / "portal-steps.generated.yaml").read_text())
    fields = dict(json.loads((REHEARSALS / "cila-fittizia" / "fixture-p6.json").read_text())["fields"])
    a = json.loads((d / answers).read_text())
    for k, v in changes.items():
        if v is None:
            a.pop(k, None)
        else:
            a[k] = v
    return run.preflight(steps, run.Values(a, fields=fields, asks=steps.get("asks")))


def sue_every_case_decided_once():
    """The SUE table as the library's validator holds it: every combination of the facts is
    decided by one main rule, by "none needed", or refused."""
    domains = [list(f["values"]) if f["type"] == "choice" else [True, False] for f in SUE["facts"].values()]
    n = 0
    for combo in itertools.product(*domains):
        d = rules.decide(SUE, dict(zip(SUE["facts"], combo)))
        assert not d["conflict"] and (d["main"] or d["none"] or d["refused"]), (combo, d)
        n += 1
    assert n > 100000, n


def sue_rehearsals_ready():
    names = ["cil-fittizia", "cila-fittizia", "cila-paesaggistica-fittizia", "scia-fittizia", "scia-paesaggistica-semplificata-fittizia",
             "scia-paesaggistica-ordinaria-fittizia", "pdc-fittizio", "scia-alternativa-fittizia",
             "paesaggistica-semplificata-fittizia", "paesaggistica-ordinaria-fittizia"]
    # A whole building's change of use with CILA works: the national SCIA form has a box for it,
    # but in Campania it is a SCIA alternativa (art. 2 c. 6 L.R. 13/2022).
    refused = {("scia-fittizia", "answers-cambio-immobile-6bis.json"): "napoli/comune/scia-alternativa-pdc"}
    for name in names:
        for f in sorted((REHEARSALS / name).glob("answers*.json")):
            r = sue(name, f.name)
            if (name, f.name) in refused:
                assert r["unsupported"][0].startswith("This case is another pratica: " + refused[name, f.name]), r
            else:
                assert r["ready"], (name, f.name, r)


def sue_another_pratica():
    """Each building recipe refuses a case the table gives to another pratica, or one that
    needs (or doesn't need) the landscape authorization it files with it."""
    other = "This case is another pratica: "
    cila, scia = "napoli/comune/cila-lavori-edilizi", "napoli/comune/scia-edilizia"
    later = "answers-proprietario.json"  # works not started yet: no sanatoria
    semplificata = "napoli/comune/autorizzazione-paesaggistica-semplificata"
    ordinaria = "napoli/comune/autorizzazione-paesaggistica-ordinaria"
    assert sue("cila-fittizia", later, fatti=["strutture"], sismica="deposito")["unsupported"][0].startswith(other + scia)
    assert sue("cila-fittizia", later, cambio_uso="stessa_categoria", fatti=["uso_ammesso_prg"])["unsupported"][0].startswith(
        other + scia)
    assert sue("cila-fittizia", later, paesaggio="semplificata")["unsupported"] == [
        f"This case also needs {semplificata} (autorizzazione), which this pratica doesn't file."]
    assert sue("cila-paesaggistica-fittizia", paesaggio="no")["unsupported"] == [
        f"This pratica also files {semplificata}, which this case doesn't need."]
    # The SCIA's P6 job before it touched the facades: internal works, a CILA's.
    assert sue("scia-fittizia", fatti=[])["unsupported"][0].startswith(other + cila)
    assert sue("scia-fittizia", paesaggio="semplificata")["unsupported"] == [
        f"This case also needs {semplificata} (autorizzazione), which this pratica doesn't file."]
    assert set(sue("scia-paesaggistica-semplificata-fittizia", paesaggio="ordinaria")["unsupported"]) == {
        f"This case also needs {ordinaria} (autorizzazione), which this pratica doesn't file.",
        f"This pratica also files {semplificata}, which this case doesn't need."}
    assert sue("scia-paesaggistica-ordinaria-fittizia", intervento="ristrutturazione_pesante")["unsupported"][0].startswith(
        other + "napoli/comune/scia-alternativa-pdc")
    assert sue("pdc-fittizio", intervento="manutenzione_straordinaria", fatti=[], strutture="false",
               cambio_uso="no")["unsupported"][0].startswith(other + cila)
    assert sue("scia-alternativa-fittizia", intervento="nuova_costruzione", fatti=[])["unsupported"][0].startswith(
        other + "napoli/comune/permesso-di-costruire (permesso)")
    assert sue("paesaggistica-semplificata-fittizia", intervento="manutenzione_straordinaria", fatti=[])["unsupported"][0].startswith(
        other + cila)
    assert sue("paesaggistica-ordinaria-fittizia", paesaggio="semplificata")["unsupported"][0].startswith(other + semplificata)
    # Works that need no title, work already done, and a Naples rule (DGC 454/2014).
    assert sue("paesaggistica-semplificata-fittizia", paesaggio="no")["unsupported"][0].startswith("This case needs no pratica: ")
    assert "sanatoria" in sue("scia-fittizia", fatti=["prospetti", "sanatoria"])["unsupported"][0]
    assert "accertamento di compatibilità" in sue("cila-paesaggistica-fittizia", fatti=["paesaggio_eseguiti"])["unsupported"][0]
    assert sue("scia-fittizia", "answers-variante-pdc.json", fatti=["demolizione_zona_a"])["unsupported"][0].startswith(
        other + "napoli/comune/permesso-di-costruire")
    assert "DGC 454/2014" in sue("scia-alternativa-fittizia", fatti=["demolizione_zona_a"])["unsupported"][0]
    # Changes of use under Campania's laws (circolare regionale CI/2025/3).
    unit = {"fatti": ["uso_ammesso_prg"]}
    assert sue("scia-fittizia", "answers-cambio-unita-senza-tra.json", intervento="nessuna_opera")["unsupported"][0].startswith(
        "This case needs no pratica: ")  # art. 2 c. 5 L.R. 19/2001
    assert sue("scia-fittizia", cambio_uso="non_rilevante", fatti=["prospetti", "uso_ammesso_prg"])["ready"]  # the facades: a SCIA
    assert sue("scia-fittizia", cambio_uso="intero_immobile", **unit)["unsupported"][0].startswith(
        other + "napoli/comune/scia-alternativa-pdc")
    assert sue("scia-alternativa-fittizia", cambio_uso="intero_tra_categorie", fatti=["uso_ammesso_prg", "cambio_uso_sagoma"])[
        "unsupported"][0].startswith(other + "napoli/comune/permesso-di-costruire")  # art. 2 c. 6-7 L.R. 19/2001
    assert sue("scia-fittizia", cambio_uso="zona_agricola", **unit)["unsupported"][0].startswith(
        other + "napoli/comune/permesso-di-costruire")
    assert "art. 27 c. 2" in sue("scia-fittizia", cambio_uso="stessa_categoria", fatti=["prospetti"])["unsupported"][0]
    # Temporary works: the CIL, alone (art. 6 c. 1 e-bis); not on a bene culturale, not afterwards.
    cil = "napoli/comune/cil-opere-temporanee"
    assert sue("cila-fittizia", later, intervento="opere_temporanee", fatti=[])["unsupported"][0].startswith(other + cil)
    assert sue("cil-fittizia", intervento="manutenzione_straordinaria", fatti=[], cambio_uso="no")["unsupported"][0].startswith(
        other + cila)
    assert sue("cil-fittizia", paesaggio="semplificata")["unsupported"] == [
        f"This case also needs {semplificata} (autorizzazione), which this pratica doesn't file."]
    assert "soprintendente" in sue("cil-fittizia", fatti=["bene_culturale"])["unsupported"][0]
    assert "sanatoria" in sue("cil-fittizia", fatti=["sanatoria"])["unsupported"][0]
    # Naples: an ordinary home turned B&B or short let in the centro storico's tavola 7a area
    # (D.C.C. 129/2026), whose 70% threshold has no verified checking mechanism yet.
    a2 = sue("scia-fittizia", "answers-cambio-unita-senza-stessa.json", fatti=["uso_ammesso_prg", "verso_a2_tavola_7a"])
    assert len(a2["unsupported"]) == 1 and "tavola 7a" in a2["unsupported"][0] and "70%" in a2["unsupported"][0], a2
    ground = sue("scia-fittizia", cambio_uso="piano_terra", **unit)["unsupported"]
    assert len(ground) == 1 and "1-quater" in ground[0] and "va chiesto al SUE" in ground[0], ground


def sue_asks_and_apart():
    # What the works are is asked alone first: the other facts depend on it.
    r = sue("cila-fittizia", intervento=None, fatti=None, paesaggio=None, cambio_uso=None)
    assert [q["key"] for q in r["questions"]] == ["intervento"], r["questions"]
    # Then, in one message, the facts the rules for those works still wait on.
    r = sue("cila-fittizia", fatti=None, paesaggio=None, cambio_uso=None)
    assert [q["key"] for q in r["questions"]] == ["fatti", "sismica", "cambio_uso", "paesaggio"], r["questions"]
    assert [i["key"] for i in r["questions"][0]["items"]] == ["strutture", "prospetti", "uso_ammesso_prg", "verso_a2_tavola_7a",
                                                               "paesaggio_eseguiti", "bene_culturale"], r["questions"][0]
    assert "permesso_per_scelta" not in str(r["questions"])  # the recipe's: a CILA is no choice
    # The seismic filing is the person's, at the Genio Civile: in their steps, never a refusal.
    r = sue("scia-fittizia", "answers-strutture.json")
    assert r["ready"] and r["yours"][-1] == ("File apart, with its own office: "
                                             "napoli/genio-civile/deposito-autorizzazione-sismica (deposito)"), r["yours"]
    assert not [y for y in sue("scia-fittizia")["yours"] if y.startswith("File apart")]


def raises(fn, words, error=run.StepError):
    try:
        fn()
    except error as e:
        assert words in str(e), str(e)
        return
    raise AssertionError(f"no error ({words!r} expected)")


def gen_checks():
    asks, groups = {"insegna": {"type": "text", "question": "Il nome sull'insegna"}}, {}
    raises(lambda: gen.rules_block({"table": "suap-napoli", "facts": {"attivita": "somministrazione"}}, copy.deepcopy(asks), {}),
           "the fact insegna is also asks.insegna, a text", gen.GenError)
    raises(lambda: gen.rules_block({"table": "suap-napoli", "facts": {"tipo": "x"}}, {}, {}), "has no fact tipo", gen.GenError)
    raises(lambda: gen.rules_block({"table": "suap-napoli", "facts": {"evento": "riapertura"}}, {}, {}),
           "isn't a value of the fact evento", gen.GenError)
    raises(lambda: gen.rules_block({"table": "nessuna"}, {}, {}), "no rules table nessuna", gen.GenError)
    asks = {}
    block = gen.rules_block({"table": "suap-napoli", "facts": {"attivita": "somministrazione", "insegna": "{{ask.x}}"},
                             "handles": ["napoli/comune/insegna-esercizio"]}, asks, groups)
    assert block["facts"]["attivita"] == "somministrazione" and block["facts"]["insegna"] == "{{ask.x}}"
    assert block["facts"]["zona"] == "{{ask.zona}}" and block["handles"] == ["napoli/comune/insegna-esercizio"]
    assert asks["alcolici"]["group"] == "fatti" and "fatti" in groups and "insegna" not in asks, asks
    # An ask's options are the table's values written with spaces ("senza autorizzazione"); a
    # combination's `main` is passed on, and the recipe words the checklist.
    asks, groups = {"paesaggio": {"type": "choice", "options": ["no", "senza autorizzazione", "semplificata", "ordinaria"]}}, {}
    block = gen.rules_block({"table": "sue-napoli", "main": "napoli/comune/cila-lavori-edilizi", "question": "Per i lavori?",
                             "facts": {"permesso_per_scelta": "false"}}, asks, groups)
    assert block["facts"]["paesaggio"] == "{{ask.paesaggio}}" and block["main"] == "napoli/comune/cila-lavori-edilizi"
    assert groups["fatti"]["question"] == "Per i lavori?" and "question" not in block, (block, groups)
    raises(lambda: gen.rules_block({"table": "sue-napoli"}, {"paesaggio": {"type": "choice", "options": ["no", "forse"]}}, {}),
           "the fact paesaggio is also asks.paesaggio", gen.GenError)


def same_as_library():
    lib = os.environ.get("PRATICHE_NAPOLI_DIR")
    if not lib:
        print("  (PRATICHE_NAPOLI_DIR unset: rules/ not compared with the library's)")
        return
    for p in sorted(rules.TABLES.glob("*.yaml")):
        theirs = Path(lib) / "rules" / p.name
        assert theirs.is_file() and theirs.read_bytes() == p.read_bytes(), f"rules/{p.name} differs from {theirs}"


def main():
    os.chdir(REHEARSALS.parent)  # the rehearsals' files are paths from the repository
    tests = [every_case_decided_once, decide_answers, ready_as_proven, facts_asked, refused_by_the_table, other_pratiche,
             gen_checks, sue_every_case_decided_once, sue_rehearsals_ready, sue_another_pratica, sue_asks_and_apart,
             same_as_library]
    for t in tests:
        t()
        print(f"ok  {t.__name__}")
    print(f"{len(tests)}/{len(tests)} passed")


if __name__ == "__main__":
    main()
