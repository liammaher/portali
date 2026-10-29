"""The pratiche-napoli library's rules tables (rules/*.yaml, a copy of the library's
rules/): from a few facts about a case to the pratiche the law asks for.

decide() reads a table as the library's tools/lib/rules.ts does (the library validates
the tables: every combination of facts decided once, every rule citing a verified claim).
bench/test_rules.py holds the two to the same answers and the copy to the library's.
"""

from pathlib import Path

import yaml

HERE = Path(__file__).resolve().parent
TABLES = HERE / "rules"
_tables = {}


def table(name):
    if name not in _tables:
        path = TABLES / f"{name}.yaml"
        if not path.is_file():
            raise ValueError(f"no rules table {name} in {TABLES}")
        _tables[name] = yaml.load(path.read_text(), Loader=yaml.BaseLoader)  # strings: "true" stays a word
        for r in _tables[name]["rules"]:
            r["when"] = {k: _condition(v) for k, v in r["when"].items()}
    return _tables[name]


def _condition(c):
    """A condition as read with every scalar a string: a list, a yes/no, or a bound."""
    if isinstance(c, list):
        return c
    if isinstance(c, dict):
        return {k: float(v) for k, v in c.items()}
    return c == "true"


def _holds(cond, value):
    if value is None:
        return None
    if isinstance(cond, list):
        return str(value) in cond
    if isinstance(cond, bool):
        return value is cond
    return ("over" not in cond or value > cond["over"]) and ("upto" not in cond or value <= cond["upto"])


def fact_value(fact, v):
    """An answer as the table compares it (a bool, a number, a value's key), or None when
    it isn't one."""
    s = str(v).strip().lower()
    if fact["type"] == "yes_no":
        return {"true": True, "false": False, "yes": True, "no": False, "si": True, "sì": True}.get(s)
    if fact["type"] == "number":
        try:
            return float(s.replace(",", "."))
        except ValueError:
            return None
    return s if s in fact["values"] else None


def decide(t, facts):
    """What the table says of a case: the facts some rule still waits on (`ask`, in the
    table's order), the case's `main` pratica once none is missing, the others it adds
    (`also`), the rules that refuse it (`refused`) and main rules that disagree."""
    ask, fired = set(), []
    for rule in t["rules"]:
        held = [_holds(c, facts.get(name)) for name, c in rule["when"].items()]
        if False in held:
            continue
        if None in held:
            ask |= {name for name in rule["when"] if facts.get(name) is None}
            continue
        fired.append(rule)
    d = {"ask": [f for f in t["facts"] if f in ask], "main": None, "also": [], "refused": [], "conflict": []}
    mains = []
    for rule in fired:
        then = rule["then"]
        if rule["status"] != "verified":
            d["refused"].append({"rule": rule["id"], "reason": rule["note"]})
        elif "refuse" in then:
            d["refused"].append({"rule": rule["id"], "reason": then["refuse"]})
        else:
            o = {"rule": rule["id"], "procedure": then["procedure"], "regime": then["regime"]}
            if "portal" in then:
                o["portal"] = then["portal"]
            if then["role"] == "main":
                mains.append(o)
            elif not any(a["procedure"] == o["procedure"] for a in d["also"]):
                d["also"].append(o)
    if len({(m["procedure"], m["regime"]) for m in mains}) > 1:
        d["conflict"] = [m["rule"] for m in mains]
    elif mains and not d["ask"]:
        d["main"] = mains[0]
    d["also"] = [a for a in d["also"] if not d["main"] or a["procedure"] != d["main"]["procedure"]]
    return d
