#!/usr/bin/env python3
"""Offline tests of the rules tables (rules.py) and how gen.py and run.py preflight use them.

  ~/.cache/pratica-portal/venv/bin/python -I claude/skills/pratica/portal/bench/test_rules.py

No browser, vault or network: the non-food shop's and the restaurant's rehearsals, changed
one fact at a time. With PRATICHE_NAPOLI_DIR set, rules/ is also compared with the library's.
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


def same_as_library():
    lib = os.environ.get("PRATICHE_NAPOLI_DIR")
    if not lib:
        print("  (PRATICHE_NAPOLI_DIR unset: rules/ not compared with the library's)")
        return
    for p in sorted(rules.TABLES.glob("*.yaml")):
        theirs = Path(lib) / "rules" / p.name
        assert theirs.is_file() and theirs.read_bytes() == p.read_bytes(), f"rules/{p.name} differs from {theirs}"


def main():
    tests = [every_case_decided_once, decide_answers, ready_as_proven, facts_asked, refused_by_the_table, other_pratiche,
             gen_checks, same_as_library]
    for t in tests:
        t()
        print(f"ok  {t.__name__}")
    print(f"{len(tests)}/{len(tests)} passed")


if __name__ == "__main__":
    main()
