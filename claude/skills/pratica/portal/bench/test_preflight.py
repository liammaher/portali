#!/usr/bin/env python3
"""Offline tests of run.py preflight and the steps files' `asks:` and `supports:`.

  ~/.cache/pratica-portal/venv/bin/python -I claude/skills/pratica/portal/bench/test_preflight.py

No browser, vault or network: the SCIA rehearsal's steps file, fixture and answers
(rehearsals/pizzeria-fittizia/), changed one fact at a time.
"""

import copy
import json
import os
import sys
import tempfile
from pathlib import Path

import yaml

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
sys.dont_write_bytecode = True  # no __pycache__ in the skill's folder
import run  # noqa: E402

REHEARSAL = HERE.parents[4] / "rehearsals" / "pizzeria-fittizia"
STEPS = yaml.safe_load((REHEARSAL / "portal-steps.yaml").read_text())
FIELDS = dict(json.loads((REHEARSAL / "fixture.json").read_text())["fields"])
ANSWERS = json.loads((REHEARSAL / "answers.json").read_text())


def values(answers=ANSWERS, fields=FIELDS, steps=STEPS):
    return run.Values(dict(answers), fields=dict(fields), asks=steps.get("asks"))


def raises(fn, words):
    try:
        fn()
    except run.StepError as e:
        assert words in str(e), str(e)
        return
    raise AssertionError(f"no error ({words!r} expected)")


def ready():
    r = run.preflight(STEPS, values())
    assert r["ready"], r
    assert not (r["questions"] or r["missing"] or r["unsupported"]), r
    assert any("€ 120" in y for y in r["yours"]), r["yours"]


def all_questions():
    r = run.preflight(STEPS, values(answers={}))
    keys = [q["key"] for q in r["questions"]]
    assert keys == ["extras", "insegna", "superficie_somministrazione_mq"], keys
    items = [i["key"] for i in r["questions"][0]["items"]]
    assert r["questions"][0]["type"] == "checklist" and items[:4] == [
        "zona_tutelata", "alcolici", "musica", "insegna_esterna"], items
    assert len(items) == 6 and all(i["item"] for i in r["questions"][0]["items"]), items
    assert not r["ready"]


def alcohol():
    r = run.preflight(STEPS, values(answers=ANSWERS | {"extras": ["alcolici"]}))
    assert not r["ready"] and r["unsupported"] == ["Selling alcohol isn't supported yet: it adds the Dogane "
                                                   "notice to the pratica."], r
    assert not r["questions"], "asked questions for an unsupported case"


def company():
    r = run.preflight(STEPS, values(fields=FIELDS | {"business.forma_giuridica": "srl"}))
    assert r["unsupported"] and "ditta individuale" in r["unsupported"][0], r


def page_refuses():
    raises(lambda: run.page_scripts(STEPS, values(answers=ANSWERS | {"extras": ["musica"]})), "not supported: Music")
    raises(lambda: run.page_scripts(STEPS, values(answers={k: v for k, v in ANSWERS.items() if k != "extras"})),
           "unanswered: ask.alcolici")


def missing_vault_field():
    fields = {k: v for k, v in FIELDS.items() if k != "business.partita_iva"}
    r = run.preflight(STEPS, values(fields=fields))
    assert [m["field"] for m in r["missing"]] == ["business.partita_iva"], r["missing"]
    assert not r["ready"]


def no_titolare():
    fields = {k: v for k, v in FIELDS.items() if not k.endswith(":ruolo.tipo")}
    r = run.preflight(STEPS, values(fields=fields))
    assert r["missing"] and r["missing"][0]["field"] == "*:ruolo.tipo", r["missing"]


def bad_answer():
    r = run.preflight(STEPS, values(answers=ANSWERS | {"superficie_somministrazione_mq": "trenta"}))
    q = [q for q in r["questions"] if q["key"] == "superficie_somministrazione_mq"]
    assert q and q[0]["error"] == "answer with a number", r["questions"]


def default_used():
    v = values(answers={k: x for k, x in ANSWERS.items() if k != "descrizione"})
    assert v.field("ask.descrizione") == "SCIA somministrazione - " + FIELDS["business.denominazione"]


def ask_without_question():
    steps = copy.deepcopy(STEPS)
    del steps["asks"]["insegna"]
    raises(lambda: run.preflight(steps, values(steps=steps)), "no question in asks: for insegna")


def expired():
    steps = copy.deepcopy(STEPS)
    steps["sections"][0]["fields"].append({"id": "x", "kind": "text", "label": "CIE", "value": "{{t:cie.numero}}"})
    slug = run.Values({}, fields=FIELDS).titolare
    r = run.preflight(steps, values(steps=steps, fields=FIELDS | {f"{slug}:cie.numero": "CA00000AA",
                                                                  f"{slug}:cie.data_scadenza": "2020-01-01"}))
    assert any(m["why"] == "expired on 2020-01-01" for m in r["missing"]), r["missing"]


def files():
    steps = copy.deepcopy(STEPS)
    tmp = Path(tempfile.mkdtemp(prefix="preflight-"))
    big = tmp / "planimetria.pdf"
    big.write_bytes(b"%PDF" + b"0" * 300 * 1024)
    steps["asks"]["planimetria"] = {"type": "file", "question": "The planimetria (PDF)"}
    steps["sections"][-1]["fields"].append({"id": "p", "kind": "file", "label": "Planimetria",
                                            "value": "{{ask.planimetria}}"})
    r = run.preflight(steps, values(steps=steps, answers=ANSWERS | {"planimetria": str(big)}))
    assert r["ready"] and r["files"][0]["exists"] and not r["pane_ok"], r
    r = run.preflight(steps, values(steps=steps, answers=ANSWERS | {"planimetria": str(tmp / "nope.pdf")}))
    assert not r["ready"] and r["questions"][0]["error"].startswith("no file"), r
    # Over the portal's 9.77 MB, or a type the slot doesn't take: not ready, and why.
    huge = tmp / "rilievo.pdf"
    with huge.open("wb") as f:
        f.truncate(run.SLOT_MAX_BYTES + 1)
    r = run.preflight(steps, values(steps=steps, answers=ANSWERS | {"planimetria": str(huge)}))
    assert not r["ready"] and "over the portal's 9.77 MB" in r["files"][0]["problem"], r["files"]
    steps["sections"][-1]["fields"][-1]["accepts"] = "pdf.p7m;jpg.p7m"
    r = run.preflight(steps, values(steps=steps, answers=ANSWERS | {"planimetria": str(big)}))
    assert not r["ready"] and r["files"][0]["problem"] == "the slot takes only pdf.p7m, jpg.p7m", r["files"]
    signed = tmp / "planimetria.pdf.p7m"
    signed.write_bytes(b"0")
    r = run.preflight(steps, values(steps=steps, answers=ANSWERS | {"planimetria": str(signed)}))
    assert r["ready"] and "problem" not in r["files"][0], r["files"]
    for f in (big, huge, signed):
        f.unlink()
    tmp.rmdir()


def checklist_forms():
    # The items ticked as a list, or each item on its own: the same answer.
    each = {k: v for k, v in ANSWERS.items() if k != "extras"} | {
        k: k == "stagionale" for k in ("zona_tutelata", "alcolici", "musica", "insegna_esterna",
                                       "centro_commerciale", "stagionale")}
    listed = ANSWERS | {"extras": ["stagionale"]}
    a, b = run.page_scripts(STEPS, values(answers=each)), run.page_scripts(STEPS, values(answers=listed))
    assert a == b, "a list and per-item answers filled differently"
    r = run.preflight(STEPS, values(answers=ANSWERS | {"extras": ["vino"]}))
    assert r["questions"] and r["questions"][0]["key"] == "extras" and "error" in r["questions"][0], r["questions"]


def unused_ask():
    steps = copy.deepcopy(STEPS)
    steps["asks"]["colore"] = {"type": "text", "question": "Favourite colour?"}
    raises(lambda: run.preflight(steps, values(steps=steps)), "no field reads asks.colore")


def not_needed_here():
    # A question only a field this case skips would read isn't asked; one a field reads
    # only if another answer says so says which.
    steps = copy.deepcopy(STEPS)
    steps["asks"]["data_stagione"] = {"type": "date", "question": "When does the season start?"}
    steps["sections"][0]["fields"].append({"id": "s", "kind": "text", "label": "Inizio stagione",
                                           "when": "{{ask.stagionale}}", "value": "{{ask.data_stagione}}"})
    r = run.preflight(steps, values(steps=steps))
    assert r["ready"] and not r["questions"], r["questions"]
    r = run.preflight(steps, values(steps=steps, answers=ANSWERS | {"extras": ["stagionale"]}))
    assert [q["key"] for q in r["questions"]] == ["data_stagione"], r["questions"]
    r = run.preflight(steps, values(steps=steps, answers={k: v for k, v in ANSWERS.items() if k != "extras"}))
    q = [q for q in r["questions"] if q["key"] == "data_stagione"]
    assert q and q[0]["only_if"] == ["stagionale"], r["questions"]


def proposals():
    # A proposal the vault can fill is offered for confirmation, not asked; confirmed, it's
    # an answer. One the vault can't fill falls back to the question.
    steps = copy.deepcopy(STEPS)
    steps["asks"]["insegna"]["propose"] = "{{business.denominazione}}"
    no_sign = {k: v for k, v in ANSWERS.items() if k != "insegna"}
    r = run.preflight(steps, values(steps=steps, answers=no_sign))
    assert [p["key"] for p in r["proposals"]] == ["insegna"], r["proposals"]
    assert r["proposals"][0]["proposal"] == FIELDS["business.denominazione"], r["proposals"]
    assert "insegna" not in [q["key"] for q in r["questions"]] and not r["ready"], r
    confirmed = no_sign | {"insegna": r["proposals"][0]["proposal"]}
    assert run.preflight(steps, values(steps=steps, answers=confirmed))["ready"]
    fields = {k: v for k, v in FIELDS.items() if k != "business.denominazione"}
    r = run.preflight(steps, values(steps=steps, answers=no_sign, fields=fields))
    assert "insegna" in [q["key"] for q in r["questions"]] and not r["proposals"], r
    steps["asks"]["alcolici"]["propose"] = "false"
    raises(lambda: run.preflight(steps, values(steps=steps)), "no propose")


def sums():
    # A total the portal wants (rooms, beds) from what was asked: each term is a question.
    asks = {"a": {"type": "number", "question": "a", "default": "0"}, "b": {"type": "number", "question": "b"}}
    v = run.Values({"b": "3"}, fields={}, asks=asks)
    assert v.render("{{ask.a+2*ask.b}}") == "6", v.render("{{ask.a+2*ask.b}}")
    assert v.render("{{ask.a+ask.b|positive}}") == "true"
    assert run.Values({}, fields={}, asks=asks).render("{{ask.a+ask.b}}") is None  # b unanswered
    assert [r for r, _ in run.refs_in("{{ask.a+2*ask.b|positive}}")] == ["ask.a", "ask.b"]
    assert v.render("{{ask.b|atleast_3}} {{ask.b|atleast_4}}") == "true false"


def too_long():
    # A box's max is checked before the run: an answer is asked again, a vault value named.
    steps = copy.deepcopy(STEPS)
    asked = next(f for sec in steps["sections"] for f in sec["fields"]
                 if str(f.get("value", "")).startswith("{{ask.") and "when" not in f and "|" not in f["value"])
    vault = next(f for sec in steps["sections"] for f in sec["fields"]
                 if str(f.get("value", "")).startswith("{{business.denominazione") and "when" not in f)
    asked["max"], vault["max"] = 3, 3
    r = run.preflight(steps, values(steps=steps))
    key = asked["value"][6:].split("}")[0]
    assert any(q["key"] == key and "at most 3" in q.get("error", "") for q in r["questions"]), r["questions"]
    assert any("at most 3" in m["why"] for m in r["missing"]), r["missing"]
    assert not r["ready"]


def nested_when():
    """The CILA's quadro f: "oneroso, and then paid?" asks the second only once the first is
    yes; the tolerance details only once there are tolerances."""
    cila = HERE.parents[4] / "rehearsals" / "cila-fittizia"
    steps = yaml.safe_load((cila / "portal-steps.generated.yaml").read_text())
    fields = dict(json.loads((cila / "fixture-p6.json").read_text())["fields"])
    owner = json.loads((cila / "answers-proprietario.json").read_text())
    drop = {"cila_onerosa", "contributo_pagato", "ricevuta_contributo", "tolleranze", "tolleranze_tipi",
            "toll_superficie", "toll_strutturale"}
    base = {k: v for k, v in owner.items() if k not in drop}
    here = os.getcwd()
    os.chdir(HERE.parents[4])  # the answers name files from the repo's root
    try:
        ask = lambda a: {q["key"]: q for q in run.preflight(steps, values(a, fields, steps))["questions"]}
        q = ask(base)
        assert q["contributo_pagato"]["only_if"] == ["cila_onerosa"], q.get("contributo_pagato")
        assert q["toll_strutturale"]["only_if"] == ["tolleranze"], q.get("toll_strutturale")
        q = ask(base | {"cila_onerosa": False, "tolleranze": False})
        assert not {"contributo_pagato", "ricevuta_contributo", "tolleranze_tipi", "toll_superficie", "toll_strutturale"} & set(q), q
        q = ask(base | {"cila_onerosa": True, "tolleranze": True})
        assert "only_if" not in q["contributo_pagato"] and q["ricevuta_contributo"]["only_if"] == ["contributo_pagato"], q
    finally:
        os.chdir(here)


def linked_files():
    """A file over the pane's 256 KB is pane_ok once it goes by link (page --file-urls)."""
    with tempfile.TemporaryDirectory() as tmp:
        big = Path(tmp) / "planimetria.pdf"
        big.write_bytes(b"%PDF-1.4\n" + b"0" * (300 * 1024))
        steps = copy.deepcopy(STEPS)
        steps["sections"][0]["fields"].append({"kind": "file", "label": "Planimetria", "value": str(big)})
        assert not run.preflight(steps, values(steps=steps))["pane_ok"]
        r = run.preflight(steps, values(steps=steps), {str(big): "https://example.org/planimetria.pdf"})
        assert r["pane_ok"] and any(f.get("link") for f in r["files"]), r["files"]


CASES = [
    ("the SCIA with its answers is ready", ready),
    ("no answers: one checklist, branch items first, then two questions, no defaults", all_questions),
    ("alcohol stops at pre-flight with its reason, nothing else asked", alcohol),
    ("a company is refused: only ditte individuali are mapped", company),
    ("page_scripts refuses an unsupported or unanswered case", page_refuses),
    ("an empty vault field is named", missing_vault_field),
    ("no titolare is a missing field, not a crash", no_titolare),
    ("a bad answer is asked again with why", bad_answer),
    ("a default fills an unanswered ask", default_used),
    ("an ask with no question is a steps-file error", ask_without_question),
    ("an expired document the steps read is named", expired),
    ("files: found, too big for the pane or the portal, of a type the slot doesn't take, or missing", files),
    ("a checklist answers as a list or item by item, and a bad list is asked again", checklist_forms),
    ("a question no field reads is a steps-file error", unused_ask),
    ("a question this case doesn't need isn't asked; a conditional one says on what", not_needed_here),
    ("a proposal is offered to confirm, used once confirmed, and asked when the vault can't fill it", proposals),
    ("a sum of answers renders, needs every term, and each term is asked", sums),
    ("a value longer than its box is asked again, or named in the vault, before the run", too_long),
    ("a file over the pane's 256 KB is pane_ok once it goes by link", linked_files),
    ("a condition's words gate each other: a later question waits on the earlier answer (CILA contributo)", nested_when),
]


def main():
    failed = 0
    for name, fn in CASES:
        try:
            fn()
            print(f"ok    {name}")
        except AssertionError as e:
            failed += 1
            print(f"FAIL  {name}: {e}")
    print(f"{len(CASES) - failed}/{len(CASES)} passed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
