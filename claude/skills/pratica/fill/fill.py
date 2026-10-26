#!/usr/bin/env python3
"""Fill a pratiche-napoli form from the vault, and check the result.

  fill.py needs --entry E.json --form ID --layout L.json [--draft]
      The service tokens a run needs, as the pratica skill asks for them:
      prepare (append, the form's categories and pratiche.*) and track
      (write, pratiche.* and the categories the entry's records go to); with
      --draft, one read token for the form's categories.

  fill.py run --entry E.json --form ID --layout L.json --pdf FORM.pdf --out DIR
              (--server S.json | --vault-dir DIR [--keychain SERVICE])
              [--draft] [--answers A.json] [--reply R.json]
              [--deadline-from FIELD --deadline-days N] [--playbook NAME]
      Reads the vault over its HTTP API with the prepare token, resolves each
      of the entry's form rows, fills the PDF at the layout's positions, reads
      the PDF back to check every value landed in its box, and, only if every
      check passed and this isn't a --draft, opens the pratica record and
      checks its deadline in /vault/expiring. With --reply it then plays the
      ente's answer with the track token: sent, records saved, closed. Writes
      DIR/filled.pdf, DIR/page-1.png and DIR/report.json; exits 1 if a check
      failed. A --draft writes nothing to the vault.

The vault: --server S.json, {"url": ..., "tokens": {"prepare": ..., "track":
...}}, is the synthetic vault over TCP. --vault-dir is a real one (~/.pvault,
or ~/.pvault-<name>): requests, the token included, go only to its
run/pvault.sock, after checking that folder is yours and private, as the MCP
server does; the token is the one `pvault create-service-token ... --keychain
SERVICE` stored, else $VAULT_TOKEN.

The entry is a library procedure as JSON (its YAML, converted). Needs PyMuPDF (fitz).
"""

import argparse
import datetime as dt
import getpass
import hashlib
import http.client
import json
import os
import re
import socket
import stat
import subprocess
import sys
import urllib.parse

# The vault's word for a library channel type (it takes the library's as is,
# saving these three as the Italian word).
CANALE = {"portal": "portale", "desk": "sportello", "post": "posta"}
CLOSED = {"completata", "respinta", "annullata"}
MAP_KINDS = {"direct", "concat", "fallback", "repeat", "ask", "blank_with_reason"}
INK = (0, 0, 0.55)
# A Unicode font to embed when there is one, so Łukasz or Nguyễn print; the
# base-14 Helvetica only has Latin-1. Neither is used by the forms here, so
# text in it is ours (read_back relies on that).
UNICODE_FONTS = [
    "/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
    "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
    "/usr/share/fonts/dejavu/DejaVuSans.ttf",
]
# Answers confirming that nobody else lives in the home.
NOBODY = {"nessuno", "nessuna", "none", "nobody"}
# Typographic marks typed on phones, as the plain ones Latin-1 has.
PLAIN = str.maketrans({"’": "'", "‘": "'", "“": '"', "”": '"', "–": "-"})


def said(answer):
    """An answer as compared with a layout's list: "No." is no, "Sì" is sì."""
    return str(answer).translate(PLAIN).strip().rstrip(".!").strip().lower()


# --- The entry and its rows -------------------------------------------------

def load(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def form_of(entry, form_id):
    for form in entry.get("forms", []):
        if form["id"] == form_id:
            return form
    sys.exit(f"{entry['id']} has no form {form_id}")


def check_supported(entry, form):
    # The vault owner is the dichiarante and the family are the conviventi:
    # what a personal vault's resident procedure needs. Business vaults
    # (dichiarante, party, repeat by ruolo) come later. Anything else is
    # refused, never read as blank.
    if entry.get("schema_version") != 0:
        sys.exit(f"schema_version {entry.get('schema_version')}: this tool reads version 0")
    if "dichiarante" in entry:
        sys.exit("procedures with a dichiarante aren't supported yet")
    for row in form["fields"]:
        m = row["map"]
        kinds = set(m) & MAP_KINDS
        if "party" in m or isinstance(m.get("repeat"), dict):
            sys.exit(f"row {row['label']!r}: party and repeat by ruolo aren't supported yet")
        if len(kinds) != 1 or set(m) - MAP_KINDS - {"fields"}:
            sys.exit(f"row {row['label']!r}: unknown map {sorted(m)}")
        if m.get("repeat", "famiglia") != "famiglia":
            sys.exit(f"row {row['label']!r}: unknown repeat {m['repeat']!r}")


def refs(m):
    return [m["direct"]] if "direct" in m else m.get("concat", []) + m.get("fallback", [])


def category(ref):
    local = ref.split(":", 1)[1] if ":" in ref.split(".", 1)[0] else ref
    return local.split(".", 1)[0]


def template_refs(layout):
    """Vault fields a layout template names directly ({abitazione.piano})."""
    out = set()
    for box in layout["boxes"]:
        for name in re.findall(r"\{([^}|]+)", box.get("template", "")):
            if "." in name:
                out.add(name)
    return out


def needs(entry, form, layout, draft=False):
    check_supported(entry, form)
    patterns = set()
    for row in form["fields"]:
        m = row["map"]
        for ref in refs(m):
            patterns.add(f"{category(ref)}.*")
        if m.get("repeat") == "famiglia":
            # Who is in the stato di famiglia, and their names for relazione_label.
            patterns |= {"*:famiglia.*", "*:identity.*"}
            patterns |= {f"*:{category(ref)}.*" for ref in m["fields"]}
    patterns |= {f"{category(ref)}.*" for ref in template_refs(layout)}
    # *:x.* reaches the owner too, so x.* beside it adds nothing.
    patterns = {p for p in patterns if f"*:{p}" not in patterns}
    if draft:
        return {"tokens": {"draft": {"scope": ",".join(sorted(patterns)), "access": "read"}}}
    records = {f"{category(r['field'])}.*" for r in entry.get("records", [])}
    return {
        "tokens": {
            "prepare": {"scope": ",".join(sorted(patterns) + ["pratiche.*"]), "access": "append"},
            "track": {"scope": ",".join(["pratiche.*"] + sorted(records)), "access": "write"},
        }
    }


# --- The vault, over HTTP ----------------------------------------------------

class UnixHTTPConnection(http.client.HTTPConnection):
    def __init__(self, socket_path, timeout):
        super().__init__("localhost", timeout=timeout)
        self.socket_path = socket_path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.socket_path)


def vault_socket(vault_dir):
    """<vault dir>/run/pvault.sock, once it is this user's vault server's:
    the run folder ours and private (0700), the socket ours. Anything can
    listen on 127.0.0.1:7200 while the vault is locked; only this user can
    put a socket there. The MCP server makes the same check."""
    run = os.path.join(vault_dir, "run")
    sock = os.path.join(run, "pvault.sock")
    try:
        folder, entry = os.lstat(run), os.lstat(sock)
    except FileNotFoundError:
        sys.exit(f"no vault server for {vault_dir}: run pvault unlock")
    if not stat.S_ISDIR(folder.st_mode) or folder.st_uid != os.getuid() or folder.st_mode & 0o077:
        sys.exit(f"refusing to send the token: {run} must be a private folder of yours (mode 0700)")
    if not stat.S_ISSOCK(entry.st_mode) or entry.st_uid != os.getuid():
        sys.exit(f"refusing to send the token: {sock} is not your vault server's socket")
    return sock


def keychain_token(service):
    """What `pvault create-service-token --keychain SERVICE` stored for this user."""
    if not re.fullmatch(r"[A-Za-z0-9._][A-Za-z0-9._-]{0,63}", service):
        sys.exit(f"{service!r} isn't a Keychain service name")
    # By absolute path, so nothing earlier in PATH can stand in for it.
    got = subprocess.run(["/usr/bin/security", "find-generic-password", "-a", getpass.getuser(), "-s", service, "-w"],
                         capture_output=True, text=True, timeout=10)
    if got.returncode == 44:
        sys.exit(f"nothing in Keychain service {service!r}: create the token with pvault create-service-token ... --keychain {service}")
    if got.returncode != 0:
        sys.exit(f"reading Keychain service {service!r} failed: {got.stderr.strip()}")
    return got.stdout.strip()


def connect(a):
    """The vault, and its tokens by name (prepare, track; a real vault has one
    token for both)."""
    if not a.vault_dir:
        server = load(a.server)
        return {name: Vault(token, url=server["url"]) for name, token in server["tokens"].items()}
    sock = vault_socket(os.path.expanduser(a.vault_dir))
    token = keychain_token(a.keychain) if a.keychain else os.environ.get("VAULT_TOKEN", "").strip()
    if not re.fullmatch(r"[\x21-\x7e]+", token):
        sys.exit("no service token: pass --keychain SERVICE, or set VAULT_TOKEN")
    vault = Vault(token, socket_path=sock)
    status, info = vault.call("GET", "/vault/status")
    if status != 200 or info.get("locked", True):
        sys.exit(f"the vault in {a.vault_dir} is locked: run pvault unlock")
    # As the MCP server: a set VAULT_ADDR must be this vault's own server.
    addr = os.environ.get("VAULT_ADDR", "").strip()
    if addr and urllib.parse.urlsplit(addr).netloc != info.get("addr"):
        sys.exit(f"VAULT_ADDR {addr} isn't the server of {a.vault_dir}, which listens on {info.get('addr')}")
    return {"prepare": vault, "track": vault, "draft": vault}


class Vault:
    """The vault's HTTP API: over TCP to the synthetic vault, or over a real
    vault's socket (vault_socket checked it)."""

    def __init__(self, token, url=None, socket_path=None):
        self.token, self.url, self.socket_path = token, url, socket_path

    def call(self, method, path, body=None):
        data = json.dumps(body).encode() if body is not None else None
        headers = {"Authorization": "Bearer " + self.token, "Content-Type": "application/json"}
        if self.socket_path:
            conn = UnixHTTPConnection(self.socket_path, timeout=15)
            headers["Host"] = "localhost"  # the server wants a loopback Host
        else:
            u = urllib.parse.urlsplit(self.url)
            conn = http.client.HTTPConnection(u.hostname, u.port, timeout=15)
        try:
            conn.request(method, path, body=data, headers=headers)
            resp = conn.getresponse()
            status, raw = resp.status, resp.read()
        except (OSError, http.client.HTTPException) as e:
            sys.exit(f"the vault didn't answer {method} {path}: {e}")
        finally:
            conn.close()
        try:
            return status, json.loads(raw or b"null")
        except ValueError:
            return status, raw.decode(errors="replace").strip()

    def field_path(self, field_id):
        return "/vault/fields/" + urllib.parse.quote(field_id, safe=":")

    def values(self):
        """Every field the token may read, in one request (the server leaves
        out what the token can't see)."""
        status, bundle = self.call("GET", "/vault/context")
        if status != 200:
            sys.exit(f"reading the vault: {status} {bundle}")
        return {f["id"]: f["value"] for fields in bundle["categories"].values() for f in fields}

    def why_missing(self, field_id):
        """A field the run needs and didn't get: absent, or hidden from this token."""
        status, got = self.call("GET", self.field_path(field_id))
        if status == 404:
            return "not in the vault"
        if status == 200:
            return "in the vault, but not read"
        return f"in the vault but this token can't read it ({status}: {got.get('error') if isinstance(got, dict) else got})"

    def set(self, field_id, value):
        return self.call("PUT", self.field_path(field_id), {"value": value})

    def get(self, field_id):
        status, got = self.call("GET", self.field_path(field_id))
        return got["value"] if status == 200 else None


# --- Resolving rows ----------------------------------------------------------

def resolve(form, values, household, answers):
    """Each row as value, missing (the vault lacks it: ask the user), answered,
    ask (not answered yet) or blank, the four ways the skill reads a row."""
    rows = []
    for row in form["fields"]:
        m, out = row["map"], {"label": row["label"]}
        if "direct" in m or "concat" in m:
            got = [values.get(r) for r in refs(m)]
            missing = [r for r, v in zip(refs(m), got) if not v]
            if missing:
                out.update(status="missing", missing=missing)
            else:
                out.update(status="value", value=" ".join(got))
        elif "fallback" in m:
            first = next((r for r in m["fallback"] if values.get(r)), None)
            if first:
                out.update(status="value", value=values[first], source=category(first))
            else:
                out.update(status="missing", missing=m["fallback"])
        elif "repeat" in m:
            people, unknown = [], []
            for member in household.get("members", []):
                stato = member.get("famiglia", {}).get("stato_famiglia")
                if stato is None:
                    # Unset isn't no: who lives with the dichiarante can't be guessed.
                    unknown.append(f"{member['slug']}:famiglia.stato_famiglia")
                if stato != "si":
                    continue
                person = {"slug": member["slug"], "relazione_label": member.get("relazione_label", "")}
                for ref in m["fields"]:
                    local = ref.split(":", 1)[1]
                    person[local.split(".", 1)[1]] = values.get(f"{member['slug']}:{local}", "")
                people.append(person)
            # The entry's step 2: whoever is intestatario della scheda declares.
            intestatario = values.get("famiglia.intestatario_scheda")
            if unknown:
                out.update(status="missing", missing=unknown)
            elif people and intestatario is None:
                out.update(status="missing", missing=["famiglia.intestatario_scheda"])
            elif intestatario not in (None, "io"):
                sys.exit(f"the intestatario della scheda is {intestatario}: they sign, which this tool doesn't do yet")
            elif not people and said(answers.get(row["label"], "")) not in NOBODY:
                # An empty stato di famiglia may only mean nobody was added to the vault.
                out.update(status="ask", question="Nessuno nello stato di famiglia del vault: confermare che non convive "
                           "nessuno (risposta \"nessuno\"), oppure aggiungere i conviventi al vault")
            else:
                out.update(status="value", value=people)
        elif "ask" in m:
            answer = answers.get(row["label"])
            if answer:
                out.update(status="answered", value=answer)
            else:
                out.update(status="ask", question=m["ask"])
        else:
            out.update(status="blank", reason=m["blank_with_reason"])
        rows.append(out)
    return rows


def required_by(rule, lookup):
    """Why an optional row or field is needed after all: the first of its
    required_if conditions the data meets, or None."""
    for cond in rule.get("required_if", []) if isinstance(rule, dict) else []:
        if re.search(cond["matches"], lookup(cond["field"]) or ""):
            return f"{cond['field']} {cond['means']}"
    return None


def parse_template(template):
    """{name}, {name|date}, [optional part], and \\[ \\] \\{ \\} for the
    characters themselves; groups nest."""
    stack, i = [[]], 0
    while i < len(template):
        c = template[i]
        if c == "\\" and i + 1 < len(template):
            stack[-1].append(("text", template[i + 1]))
            i += 2
            continue
        if c == "{":
            end = template.index("}", i)
            name, _, flt = template[i + 1:end].partition("|")
            stack[-1].append(("var", name, flt))
            i = end + 1
            continue
        if c == "[":
            stack.append([])
        elif c == "]":
            group = stack.pop()
            stack[-1].append(("group", group))
        else:
            stack[-1].append(("text", c))
        i += 1
    if len(stack) != 1:
        raise ValueError(f"unbalanced [ in {template!r}")
    return stack[0]


def render(template, lookup):
    """The text, and the names left empty outside any [...] (each value is
    put in once, never read as a template itself)."""
    def ev(nodes):
        out, missing = [], []
        for node in nodes:
            if node[0] == "text":
                out.append(node[1])
            elif node[0] == "var":
                v = lookup(node[1]) or ""
                if node[2] == "date" and re.fullmatch(r"\d{4}-\d{2}-\d{2}", v):
                    v = "/".join(reversed(v.split("-")))
                if not v:
                    missing.append(node[1])
                out.append(v)
            else:
                text, empty = ev(node[1])
                if not empty:
                    out.append(text)
        return "".join(out), missing
    return ev(parse_template(template))


# --- Filling and reading back -----------------------------------------------

class Pen:
    """Writes on page 1 in one font, shrinking text to fit its box."""

    def __init__(self, fitz, page, size):
        self.fitz, self.page, self.size = fitz, page, size
        self.file = next((f for f in UNICODE_FONTS if os.path.exists(f)), None)
        if self.file:
            page.insert_font(fontname="pvfill", fontfile=self.file)
            self.font, self.fontname = fitz.Font(fontfile=self.file), "pvfill"
        else:
            self.font, self.fontname = fitz.Font("helv"), "helv"
        self.written, self.problems = [], []

    def name(self):
        return self.font.name

    def fit(self, text, width):
        s = self.size
        while self.font.text_length(text, fontsize=s) > width - 2 and s > 5.5:
            s -= 0.25
        return s, self.font.text_length(text, fontsize=s)

    def put(self, rect, text, center=False):
        if not self.file:
            text = text.translate(PLAIN)
            if any(ord(c) > 255 for c in text):
                self.problems.append(f"{text!r} has characters Helvetica can't print, and no Unicode font was found")
        x0, y0, x1, y1 = rect
        s, width = self.fit(text, x1 - x0)
        if width > x1 - x0:
            self.problems.append(f"{text!r} doesn't fit its box even at {s}pt")
        x = x0 + (x1 - x0 - width) / 2 if center else x0 + 1
        self.page.insert_text((x, y1 - 2.6), text, fontname=self.fontname, fontsize=s, color=INK)
        self.written.append({"rect": rect, "text": text})

    def lines(self, rects, text):
        """Text over a box and its continuation lines, word by word; shrunk
        only if the words don't fit on all of them."""
        s = self.size
        while True:
            words, out = text.split(), []
            for x0, _, x1, _ in rects:
                line = ""
                while words and self.font.text_length((line + " " + words[0]).strip(), fontsize=s) <= x1 - x0 - 2:
                    line = (line + " " + words.pop(0)).strip()
                out.append(line)
            if not words or s <= 5.5:
                break
            s -= 0.25
        if words:
            self.problems.append(f"{text!r} doesn't fit its {len(rects)} lines")
        saved, self.size = self.size, s
        for rect, line in zip(rects, out):
            if line:
                self.put(rect, line)
        self.size = saved


def fill_pdf(fitz, pdf_in, pdf_out, layout, rows, values):
    by_label = {r["label"]: r for r in rows}
    doc = fitz.open(pdf_in)
    pen = Pen(fitz, doc[0], layout.get("font_size", 9))

    for box in layout["boxes"]:
        labels = box.get("labels") or [box["label"]]
        row = by_label.get(labels[0])
        if "skip" in box or row is None or row["status"] in ("missing", "ask", "blank"):
            continue
        value = row["value"]
        if "cells" in box:
            for (cx0, cx1), ch in zip(box["cells"], value):
                pen.put([cx0, box["baseline"] - 9, cx1, box["baseline"] + 2.6], ch, center=True)
            if len(value) != len(box["cells"]):
                pen.problems.append(f"{labels[0]}: {len(value)} characters for {len(box['cells'])} cells")
        elif "parts" in box:
            parts = value.split("-")[::-1]  # YYYY-MM-DD → DD, MM, YYYY
            for rect, part in zip(box["parts"], parts):
                pen.put(rect, part, center=True)
        elif "choices" in box:
            choice = value if value in box["choices"] else box["other"]["choice"]
            pen.put(box["choices"][choice], "X", center=True)
            if choice == box["other"]["choice"] and value != choice:
                pen.put(box["other"]["rect"], value)
        elif "checkbox" in box:
            if said(value) in {said(w) for w in box["checked_when"]}:
                pen.put(box["checkbox"], "X", center=True)
                if "then_by_hand" in box:
                    pen.problems.append(f"{labels[0]}: ticked, so {box['then_by_hand']} must be filled by hand (no library row)")
        elif "table" in box:
            table = box["table"]
            if len(value) > len(table["rows"]):
                pen.problems.append(f"{labels[0]}: {len(value)} people for {len(table['rows'])} rows")
            for (top, bottom), person in zip(table["rows"], value):
                for need in table.get("require", []):
                    why = required_by(need, lambda f: values.get(f"{person['slug']}:{f}"))
                    if why and not values.get(f"{person['slug']}:{need['field']}"):
                        pen.problems.append(f"{labels[0]}: {person['slug']} has no {need['field']} ({why})")
                for col in table["columns"]:
                    text, missing = render(col["template"], lambda n: person.get(n, ""))
                    if missing:
                        pen.problems.append(f"{labels[0]}: {person['slug']} has no {', '.join(missing)}")
                    elif text:
                        pen.put([col["x"][0], top, col["x"][1], bottom], text)
        else:
            if said(value) in {said(w) for w in box.get("skip_answers", [])}:
                continue
            label_values = {str(i): by_label.get(l, {}).get("value") for i, l in enumerate(labels)}
            source = row.get("source")

            def lookup(name):
                if name == "value":
                    return value
                if name in label_values:
                    return label_values[name]
                if name.startswith("@"):
                    return values.get(f"{source}.{name[1:]}") if source else None
                return values.get(name)
            text, missing = render(box["template"], lookup)
            if missing:
                pen.problems.append(f"{labels[0]}: no {', '.join(missing)} for {box['template']!r}")
            elif "continuation" in box:
                pen.lines([box["rect"]] + box["continuation"], text)
            else:
                pen.put(box["rect"], text)
    if pen.file:
        doc.subset_fonts()
    doc.save(pdf_out, garbage=3, deflate=True)
    return pen.written, pen.problems, pen.name()


def read_back(fitz, pdf_path, written, font):
    """Every value we wrote, read from the saved PDF in its box; and anything of
    ours found outside the boxes."""
    page = fitz.open(pdf_path)[0]
    ours = []
    for block in page.get_text("rawdict")["blocks"]:
        for line in block.get("lines", []):
            for span in line["spans"]:
                if span["font"].split("+")[-1] == font:
                    ours += [(c["bbox"], c["c"]) for c in span["chars"]]
    used, results = set(), []
    for w in written:
        x0, y0, x1, y1 = w["rect"]
        inside = [(i, b, c) for i, (b, c) in enumerate(ours)
                  if x0 - 1.5 <= (b[0] + b[2]) / 2 <= x1 + 1.5 and y0 - 1.5 <= (b[1] + b[3]) / 2 <= y1 + 1.5]
        got = "".join(c for _, _, c in sorted(inside, key=lambda t: t[1][0])).strip()
        used |= {i for i, _, _ in inside}
        results.append({"expected": w["text"], "got": got, "ok": got == w["text"]})
    stray = "".join(c for i, (_, c) in enumerate(ours) if i not in used).strip()
    return results, stray


def layout_problems(fitz, pdf_path, layout):
    """Boxes that don't sit on the blank form's own marks: a text box on its
    underscores and over no printed word, a cell on its underline, a checkbox
    on its square, a table's rows and columns on its lines. The read-back
    can't see a misplaced box; this can."""
    page = fitz.open(pdf_path)[0]
    chars = [(c["bbox"], c["c"]) for b in page.get_text("rawdict")["blocks"] for l in b.get("lines", [])
             for s in l["spans"] for c in s["chars"]]
    underscores = [b for b, c in chars if c == "_"]
    printed = [(b, c) for b, c in chars if c not in "_  "]
    shapes = [d["rect"] for d in page.get_drawings()]

    def blank_problem(rect):
        x0, top, x1, bottom = rect
        covered = sorted((max(u[0], x0), min(u[2], x1)) for u in underscores
                         if top - 1 <= (u[1] + u[3]) / 2 <= bottom + 2 and u[2] > x0 and u[0] < x1)
        width, end = 0.0, x0
        for a, b in covered:
            width, end = width + max(0.0, b - max(a, end)), max(end, b)
        if width < 0.8 * (x1 - x0):
            return "isn't on the form's blank line"
        over = "".join(c for b, c in printed
                       if x0 + 0.5 <= (b[0] + b[2]) / 2 <= x1 - 0.5 and top <= (b[1] + b[3]) / 2 <= bottom)
        return f"covers the printed {over!r}" if over else None

    def near(r, target, tol=1.0):
        return all(abs(p - q) <= tol for p, q in zip((r.x0, r.y0, r.x1, r.y1), target))

    out = []
    for box in layout["boxes"]:
        name = (box.get("labels") or [box.get("label")])[0]
        rects = ([box["rect"]] if "rect" in box else []) + box.get("continuation", []) + box.get("parts", [])
        rects += [box["other"]["rect"]] if "other" in box else []
        for rect in rects:
            problem = blank_problem(rect)
            if problem:
                out.append(f"{name}: {rect} {problem}")
        for x0, x1 in box.get("cells", []):
            if not any(abs(r.x0 - x0) <= 1 and abs(r.x1 - x1) <= 1 and 0 <= r.y0 - box["baseline"] <= 3 for r in shapes):
                out.append(f"{name}: cell {x0}-{x1} has no underline")
        squares = list(box.get("choices", {}).values()) + ([box["checkbox"]] if "checkbox" in box else [])
        for square in squares:
            if not any(near(r, square) for r in shapes):
                out.append(f"{name}: no checkbox at {square}")
        if "table" in box:
            rows = box["table"]["rows"]
            for top, bottom in rows:
                for y in (top, bottom):
                    if not any(abs(r.y0 - y) <= 0.5 and r.width > 100 for r in shapes):
                        out.append(f"{name}: no table line at y={y}")
            verticals = [r.x0 for r in shapes if r.width < 1 and r.height > 5 and rows[0][0] - 1 <= r.y0 <= rows[-1][1]]
            for col in box["table"]["columns"]:
                x0, x1 = col["x"]
                if not any(0 <= x0 - v <= 3 for v in verticals) or not any(-1 <= v - x1 <= 3 for v in verticals):
                    out.append(f"{name}: column {col['x']} isn't between the table's lines")
    return out


# --- The run -----------------------------------------------------------------

def pratiche_by_slug(values):
    out = {}
    for field_id, value in values.items():
        if field_id.startswith("pratiche."):
            slug, _, campo = field_id[len("pratiche."):].partition("_")
            out.setdefault(slug, {})[campo] = value
    return out


def run(a):
    import fitz  # PyMuPDF

    entry, layout = load(a.entry), load(a.layout)
    form = form_of(entry, a.form)
    check_supported(entry, form)
    vaults = connect(a)
    answers = load(a.answers)["answers"] if a.answers else {}
    checks = []

    def check(name, ok, detail=""):
        checks.append({"check": name, "ok": bool(ok), "detail": detail})
        print(f"  {'ok  ' if ok else 'FAIL'} {name}{': ' + detail if detail else ''}")
        return ok

    print(f"{entry['id']} #{form['id']}")
    os.makedirs(a.out, exist_ok=True)
    with open(a.pdf, "rb") as f:
        pdf_hash = hashlib.sha256(f.read()).hexdigest()
    check("form is the version the library maps", pdf_hash == form.get("sha256"), pdf_hash[:12])
    check("layout is for that version", layout["sha256"] == form.get("sha256"))
    misplaced = layout_problems(fitz, a.pdf, layout)
    check("every layout box sits on the form's own blank", not misplaced, "; ".join(misplaced))
    library_labels = {r["label"] for r in form["fields"]}
    layout_labels = {l for b in layout["boxes"] for l in (b.get("labels") or [b["label"]])}
    check("every library row has a place on the form", library_labels <= layout_labels,
          ", ".join(sorted(library_labels - layout_labels)))
    check("every layout box is a library row", layout_labels <= library_labels,
          ", ".join(sorted(layout_labels - library_labels)))

    prepare = vaults["draft"] if a.draft and "draft" in vaults else vaults["prepare"]
    values = prepare.values()
    status, household = prepare.call("GET", "/vault/members")
    check("prepare token reads the household", status == 200, str(status))
    rows = resolve(form, values, household if status == 200 else {}, answers)
    optional = layout.get("optional_rows", {})
    for r in rows:
        if r["status"] == "missing":
            # An optional row is still needed when its required_if holds (born abroad).
            rule = optional.get(r["label"])
            why = required_by(rule, values.get) if rule is not None else None
            if rule is None or why:
                check(f"row {r['label']!r} has a value", False,
                      "; ".join(f"{ref} {prepare.why_missing(ref)}" for ref in r["missing"]) + (f" (needed: {why})" if why else ""))
        if r["status"] == "ask":
            check(f"row {r['label']!r} is answered", False, r["question"])

    written, problems, font = fill_pdf(fitz, a.pdf, f"{a.out}/filled.pdf", layout, rows, values)
    check("everything the rows hold is on the form", not problems, "; ".join(problems))
    results, stray = read_back(fitz, f"{a.out}/filled.pdf", written, font)
    bad = [r for r in results if not r["ok"]]
    check(f"all {len(results)} values read back in their boxes", not bad,
          "; ".join(f"{r['expected']!r} read as {r['got']!r}" for r in bad))
    check("nothing written outside a box", not stray, stray)
    fitz.open(f"{a.out}/filled.pdf")[0].get_pixmap(dpi=110).save(f"{a.out}/page-1.png")

    procedure = entry["id"].rsplit("/", 1)[1]
    start = values.get(a.deadline_from) if a.deadline_from else None
    slug = a.slug or f"{procedure}-{(start or dt.date.today().isoformat())[:4]}"
    failed = [c["check"] for c in checks if not c["ok"]]
    ready = check("the form is ready", not failed, f"{len(failed)} checks failed" if failed else "")
    if a.draft:
        print("  draft: nothing written to the vault")
        return finish(a, entry, form, pdf_hash, slug, rows, results, checks)
    if not ready:
        return finish(a, entry, form, pdf_hash, slug, rows, results, checks)

    # As the skill's step 0: an open pratica is resumed, never opened again; a
    # closed one with this slug (a second move this year, a refused filing
    # redone) leaves the slug to it, and this one takes -2, -3...
    pratiche = pratiche_by_slug(values)
    base, n = slug, 1
    while slug in pratiche:
        stato = pratiche[slug].get("stato")
        if stato not in CLOSED:
            check(f"no pratica {slug} is open yet", False, f"stato {stato}: resume it instead")
            return finish(a, entry, form, pdf_hash, slug, rows, results, checks)
        n += 1
        slug = f"{base}-{n}"

    channels = [c for c in entry["channels"] if c["purpose"] == "procedure"]
    channel = next((c for c in channels if c.get("preferred")), channels[0])
    destinatario = channel.get("address") or channel.get("url") or channel.get("name", "")
    record = {
        "titolo": entry["title"],
        "ente": " – ".join(x for x in (entry["ente"]["name"], entry["ente"].get("office")) if x),
        "stato": "pronta",
        "canale": channel["type"],
        "destinatario": destinatario,
        "playbook": a.playbook or "",
        "prossimo_passo": f"Firmare il modulo e inviarlo ({channel['type']}: {destinatario})",
    }
    if start and a.deadline_days:
        record["data_scadenza"] = (dt.date.fromisoformat(start) + dt.timedelta(days=a.deadline_days)).isoformat()
    for campo, value in record.items():
        if value:
            status, got = prepare.set(f"pratiche.{slug}_{campo}", value)
            check(f"opens pratiche.{slug}_{campo}", status == 200, "" if status == 200 else f"{status} {got}")
    want = CANALE.get(channel["type"], channel["type"])
    check(f"canale {channel['type']!r} is saved as {want!r}", prepare.get(f"pratiche.{slug}_canale") == want)
    deadline_id = f"pratiche.{slug}_data_scadenza"

    def expiring(vault):
        status, items = vault.call("GET", "/vault/expiring?days=3650")
        return [i for i in items or [] if i["field_id"] == deadline_id] if status == 200 else None
    if "data_scadenza" in record:
        listed = expiring(prepare)
        check("the deadline shows in expiring", listed and listed[0]["expires_on"] == record["data_scadenza"],
              f"{record['data_scadenza']} ({listed[0]['days_left']} days left)" if listed else "not listed")

    if a.reply:
        # Step 6 of the skill: sent, then the ente answers.
        track = vaults["track"]
        today = dt.date.today().isoformat()
        reply = load(a.reply)["records"]
        steps = [("stato", "inviata"), ("data_invio", today)]
        steps += [(None, (field, value)) for field, value in reply.items()]
        steps += [("stato", "completata"), ("data_esito", today)]
        for campo, value in steps:
            field, value = (f"pratiche.{slug}_{campo}", value) if campo else value
            status, got = track.set(field, str(value))
            check(f"track sets {field}", status == 200, "" if status == 200 else f"{status} {got}")
        people = next((len(r["value"]) for r in rows if isinstance(r.get("value"), list)), 0)
        for field, want in (("tari.superficie_tassata_mq", values.get("abitazione.superficie_calpestabile_mq")),
                            ("tari.componenti_tassati", str(1 + people))):
            if field in reply:
                check(f"the ente's {field} matches what was declared", str(reply[field]) == want, f"{reply[field]} vs {want}")
        if "data_scadenza" in record:
            check("a closed pratica leaves expiring", expiring(track) == [])
    return finish(a, entry, form, pdf_hash, slug, rows, results, checks)


def finish(a, entry, form, pdf_hash, slug, rows, results, checks):
    report = {"entry": entry["id"], "form": form["id"], "pdf_sha256": pdf_hash, "slug": slug,
              "rows": rows, "read_back": results, "checks": checks}
    with open(f"{a.out}/report.json", "w", encoding="utf-8") as f:
        json.dump(report, f, ensure_ascii=False, indent=2)
    failed = [c for c in checks if not c["ok"]]
    print(f"{len(checks) - len(failed)}/{len(checks)} checks passed; {a.out}/filled.pdf")
    return 1 if failed else 0


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="cmd", required=True)
    for name in ("needs", "run"):
        s = sub.add_parser(name)
        s.add_argument("--entry", required=True)
        s.add_argument("--form", required=True)
        s.add_argument("--layout", required=True)
    r = sub.choices["run"]
    for s in sub.choices.values():
        s.add_argument("--draft", action="store_true", help="read only: no pratica record, no reply")
    where = r.add_mutually_exclusive_group(required=True)
    where.add_argument("--server", help="the synthetic vault's server.json")
    where.add_argument("--vault-dir", help="a real vault's folder, e.g. ~/.pvault")
    r.add_argument("--keychain", help="Keychain service holding the token (else $VAULT_TOKEN)")
    r.add_argument("--pdf", required=True)
    r.add_argument("--out", required=True)
    r.add_argument("--answers")
    r.add_argument("--reply")
    r.add_argument("--slug")
    r.add_argument("--playbook")
    r.add_argument("--deadline-from")
    r.add_argument("--deadline-days", type=int)
    a = p.parse_args()
    if a.cmd == "needs":
        entry = load(a.entry)
        print(json.dumps(needs(entry, form_of(entry, a.form), load(a.layout), a.draft), indent=2))
        return 0
    if a.draft and a.reply:
        p.error("--draft writes nothing, so it takes no --reply")
    return run(a)


if __name__ == "__main__":
    sys.exit(main())
