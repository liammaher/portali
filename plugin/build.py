#!/usr/bin/env python3
"""Build the Portali plugin for Cowork: plugin/portali plus the runner, the rehearsals'
steps files and the pratiche-napoli library, as one installable file.

  build.py [--library DIR] [--out DIR] [--connector URL]

Writes OUT/portali/ (default plugin/dist) and OUT/portali.plugin (the same, zipped), then
pre-flights the test fixture with the built plugin under the system python3, as Cowork would run
it. --library is a pratiche-napoli checkout (default $PRATICHE_NAPOLI_DIR). Needs PyYAML
(the pratica venv's Python): the plugin itself needs no packages, because every YAML file
the runner reads goes in as JSON (shim/yaml.py says how). --connector adds the Portali
connector (connector/, serving this same build) as the plugin's .mcp.json.
"""

import argparse
import datetime as dt
import hashlib
import json
import os
import random
import re
import shutil
import subprocess
import sys
import tempfile
import zipfile
from pathlib import Path

import yaml

HERE = Path(__file__).resolve().parent
REPO = HERE.parent
PRATICA = REPO / "claude" / "skills" / "pratica"
REHEARSALS = REPO / "rehearsals"
# The library's parts a Cowork session reads: its tools, reports backend and layouts
# (PDF filling, which needs PyMuPDF) stay out of this version.
LIBRARY_PARTS = ["procedures", "rules", "offices", "situations", "contract/signing.json",
                 "README.md", "LICENSE", "LICENSE-CONTENT.md"]
# What coverage.json can't derive: comuni, who files each sportello's pratiche, what's coming.
COVERAGE = yaml.safe_load((HERE / "coverage.yaml").read_text())
# The fixture the test skill runs: the CILA rehearsal's P6 case, made-up people.
TEST_CASE = "cila-fittizia"


def steps_file(d):
    """A rehearsal's steps file, as bench/rerun.py picks it: the generated one first."""
    for name in ("portal-steps.generated.yaml", "portal-steps.yaml"):
        if (d / name).is_file():
            return d / name
    return None


def notes(text):
    """The comment block a steps file starts with: who its roles are, what it fills."""
    lines = []
    for line in text.splitlines():
        if not line.startswith("#"):
            break
        lines.append(line[1:].strip())
    return " ".join(lines).strip()


def as_json(data):
    # A YAML date (2026-10-28 unquoted) goes in as the text it was, which the runner reads.
    return json.dumps(data, ensure_ascii=False, indent=1, default=str)


def copy_library(library, skill):
    for part in LIBRARY_PARTS:
        src, dst = library / part, skill / "library" / part
        if not src.exists():
            sys.exit(f"{src}: missing from the library checkout")
        if src.is_dir():
            shutil.copytree(src, dst, ignore=shutil.ignore_patterns(".*"))
        else:
            dst.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(src, dst)


def copy_runner(skill):
    portal = skill / "portal"
    portal.mkdir(parents=True)
    # catalogue.py for its drifted(): pre-flight refuses a pratica the last drift check found
    # changed on the portal, so a plugin update carries the refusal (drift.json, not the crawls).
    for name in ("run.py", "rules.py", "inpage.js", "catalogue.py"):
        shutil.copy2(PRATICA / "portal" / name, portal / name)
    (portal / "catalogue" / "F839").mkdir(parents=True)
    shutil.copy2(PRATICA / "portal" / "catalogue" / "F839" / "drift.json", portal / "catalogue" / "F839" / "drift.json")
    shutil.copy2(HERE / "shim" / "yaml.py", portal / "yaml.py")
    (portal / "rules").mkdir()
    for table in sorted((PRATICA / "portal" / "rules").glob("*.yaml")):
        # BaseLoader, as rules.py reads it: every scalar stays a string.
        (portal / "rules" / table.name).write_text(as_json(yaml.load(table.read_text(), Loader=yaml.BaseLoader)))
    (skill / "fill").mkdir()
    shutil.copy2(PRATICA / "fill" / "fill.py", skill / "fill" / "fill.py")  # run.py imports it
    shutil.copy2(PRATICA / "review.py", skill / "review.py")
    (skill / "reference").mkdir()
    shutil.copy2(REPO / "schema.json", skill / "reference" / "schema.json")


def copy_pratiche(skill, library):
    """Each rehearsal's steps file as pratiche/<name>.json, and catalog.json listing them."""
    (skill / "pratiche").mkdir()
    catalog = []
    for d in sorted(p for p in REHEARSALS.iterdir() if p.is_dir()):
        src = steps_file(d)
        if not src:
            continue
        text = src.read_text()
        steps = yaml.safe_load(text)
        out = skill / "pratiche" / f"{d.name}.json"
        out.write_text(as_json(steps))
        recipe = yaml.safe_load((d / "recipe.yaml").read_text()) if (d / "recipe.yaml").is_file() else {}
        proc = steps["procedure"]
        catalog.append({
            "name": d.name,
            "procedure": proc,
            "title": steps.get("title") or recipe.get("title"),
            "comune": recipe.get("comune") or COVERAGE["default_comune"],
            "sportello": recipe.get("sportello") or COVERAGE["sportello"].get(d.name),
            "library": f"library/procedures/{proc}.yaml" if (library / "procedures" / f"{proc}.yaml").is_file() else None,
            "refuses": [s["else"] for s in steps.get("supports", []) if "else" in s],
            "notes": notes(text),
            # The connector serves the same hash: a different one means this copy is stale.
            "steps_sha256": hashlib.sha256(out.read_bytes()).hexdigest(),
        })
    (skill / "catalog.json").write_text(as_json(catalog))
    return catalog


def last_run(d):
    """The latest date in a rehearsal README's runs table (`| 2026-10-28 | ...`): the last
    time it was run on the real portal, whatever that run's result."""
    readme = d / "README.md"
    dates = re.findall(r"^\| *(\d{4}-\d{2}-\d{2}) *\|", readme.read_text(), re.M) if readme.is_file() else []
    return max(dates, default=None)


def write_coverage(skill, catalog, library, version):
    """coverage.json: every pratica's status, from what the repo says rather than by hand.
    available: a steps file run on the real portal (its README's runs table) that the last
    drift check found unchanged; paused: one it found changed (pre-flight refuses it);
    mapped: a steps file with no recorded run yet, shown as coming soon; guide: a library
    procedure with no steps file, where Claude can say what it needs but not fill it;
    coming: coverage.yaml's list."""
    drift = json.loads((PRATICA / "portal" / "catalogue" / "F839" / "drift.json").read_text())["pratiche"]
    entries, filled = [], set()
    for e in catalog:
        steps = json.loads((skill / "pratiche" / f"{e['name']}.json").read_text())
        crawl = re.match(r"gen\.py from (\S+) ", steps.get("generated", ""))
        changed = drift.get(crawl.group(1), {}).get("differences", []) if crawl else []
        run = last_run(REHEARSALS / e["name"])
        entries.append({k: e[k] for k in ("name", "procedure", "title", "comune", "sportello", "steps_sha256")}
                       | {"status": "paused" if changed else "available" if run else "mapped", "changed": changed,
                          "avatars": COVERAGE["avatars"].get(e["sportello"], []),
                          "last_run": run, "library": bool(e["library"])})
        filled.add(e["procedure"])
    for f in sorted((library / "procedures").rglob("*.yaml")):
        proc = str(f.relative_to(library / "procedures").with_suffix(""))
        if proc in filled:
            continue
        # Only the title: the library parses its YAML with the yaml package, which takes a few
        # files PyYAML refuses.
        title = re.search(r"^title: *(.+)$", f.read_text(), re.M)
        entries.append({"name": None, "procedure": proc, "title": title and title.group(1).strip().strip('"'),
                        "comune": "F839" if proc.startswith("napoli/") else "IT", "sportello": None,
                        "status": "guide", "avatars": [], "library": True})
    entries += [c | {"name": None, "status": "coming"} for c in COVERAGE["coming"]]
    (skill / "coverage.json").write_text(as_json({
        "plugin_version": version, "built": dt.date.today().isoformat(),
        "comuni": COVERAGE["comuni"] | {"IT": "nazionale"}, "pratiche": entries}))
    return entries


def make_test_case(test):
    """The CILA rehearsal's P6 fixture, split as an architect's folder holds it: the studio
    and the architect in studio.json, the job's people in values.json."""
    d = REHEARSALS / TEST_CASE
    fixture = json.loads((d / "fixture-p6.json").read_text())
    fields = dict(fixture["fields"])
    ruolo = {k.split(":")[0]: v for k, v in fields.items() if k.endswith(":ruolo.tipo")}
    studio_people = {s for s, v in ruolo.items() if "titolare" in v.split(",")}
    studio = {k: v for k, v in fields.items() if ":" not in k or k.split(":")[0] in studio_people}
    job = {k: v for k, v in fields.items() if k not in studio}
    case = test / "cila"
    (case / "files").mkdir(parents=True)
    for f in (d / "files").glob("*.pdf"):
        shutil.copy2(f, case / "files" / f.name)
    answers = json.loads((d / "answers-p6.json").read_text())
    for k, v in answers.items():
        if isinstance(v, str) and v.startswith(f"rehearsals/{TEST_CASE}/files/"):
            answers[k] = "files/" + Path(v).name
    answers["descrizione"] = "PROVA NON INVIARE - CILA Cowork test"
    for name, data in (("studio.json", studio), ("values.json", job), ("answers.json", answers)):
        (case / name).write_text(json.dumps(data, indent=1, ensure_ascii=False))
    make_pdf(test / "planimetria-3mb.pdf", 3 * 1024 * 1024)


def make_pdf(path, size):
    """A valid one-page PDF of about `size` bytes: the padding is an unreferenced stream of
    random bytes (seeded, so each build is the same), which no compressor shrinks."""
    page = b"BT /F1 24 Tf 72 720 Td (Planimetria di prova - PROVA NON INVIARE) Tj ET"
    pad = random.Random(0).randbytes(size)
    objs = [b"<< /Type /Catalog /Pages 2 0 R >>",
            b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
            b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R "
            b"/Resources << /Font << /F1 5 0 R >> >> >>",
            b"<< /Length %d >>\nstream\n" % len(page) + page + b"\nendstream",
            b"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
            b"<< /Length %d >>\nstream\n" % len(pad) + pad + b"\nendstream"]
    out, offsets = bytearray(b"%PDF-1.4\n%\xe2\xe3\xcf\xd3\n"), []
    for i, body in enumerate(objs, 1):
        offsets.append(len(out))
        out += b"%d 0 obj\n" % i + body + b"\nendobj\n"
    xref = len(out)
    out += b"xref\n0 %d\n0000000000 65535 f \n" % (len(objs) + 1)
    out += b"".join(b"%010d 00000 n \n" % o for o in offsets)
    out += b"trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n" % (len(objs) + 1, xref)
    path.write_bytes(bytes(out))


def self_check(plugin):
    """The test case through check.py and pre-flight under python3, as Cowork runs them."""
    skill = plugin / "skills" / "portali-pratica"
    case = plugin / "skills" / "portali-cowork-test" / "cila"
    py = shutil.which("python3") or sys.executable
    with tempfile.TemporaryDirectory() as tmp:
        values = Path(tmp) / "values.json"
        r = subprocess.run([py, skill / "check.py", "studio.json", "values.json", "--out", values],
                           cwd=case, capture_output=True, text=True)
        if r.returncode:
            sys.exit(f"check.py refused the test case:\n{r.stdout}{r.stderr}")
        r = subprocess.run([py, skill / "portal" / "run.py", "preflight", "--steps",
                            skill / "pratiche" / f"{TEST_CASE}.json", "--answers", "answers.json",
                            "--values", values], cwd=case, capture_output=True, text=True)
        if r.returncode:
            sys.exit(f"pre-flight of the test case isn't ready:\n{r.stdout}{r.stderr}")
        # Every pratica's JSON must read as its YAML did: pre-flight prints the same, from
        # the plugin under python3 and from the repo's runner with PyYAML.
        for steps in sorted((skill / "pratiche").glob("*.json")):
            d = REHEARSALS / steps.stem
            answers = next(iter(sorted(d.glob("answers*.json"))), None)
            tail = (["--answers", answers] if answers else []) + ["--values", values]
            ours = subprocess.run([py, skill / "portal" / "run.py", "preflight", "--steps", steps] + tail,
                                  cwd=REPO, capture_output=True, text=True)
            theirs = subprocess.run([sys.executable, PRATICA / "portal" / "run.py", "preflight", "--steps",
                                     steps_file(d)] + tail, cwd=REPO, capture_output=True, text=True)
            if (ours.returncode, ours.stdout, ours.stderr) != (theirs.returncode, theirs.stdout, theirs.stderr):
                sys.exit(f"{steps.name}: pre-flight differs from {steps_file(d).relative_to(REPO)}'s")


PVAULT_TARGETS = [("darwin", "amd64", "tar.gz"), ("darwin", "arm64", "tar.gz"), ("linux", "amd64", "tar.gz"),
                  ("linux", "arm64", "tar.gz"), ("windows", "amd64", "zip"), ("windows", "arm64", "zip")]


def write_pvault_release(setup, version, checksums):
    """The release the setup skill installs: its version and the SHA-256 of each of its six
    archives (goreleaser's checksums.txt), shipped in the plugin so the installer checks a
    download against what the plugin says, not against what the download says."""
    if not re.fullmatch(r"\d+\.\d+\.\d+([-+][0-9A-Za-z.-]+)?", version):
        sys.exit(f"--pvault-version {version!r} isn't a version like 0.1.0")
    wanted = {f"pvault_{version}_{os_}_{arch}.{ext}" for os_, arch, ext in PVAULT_TARGETS}
    lines = {}
    for line in checksums.read_text().splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[1] in wanted:
            if not re.fullmatch(r"[0-9a-f]{64}", parts[0]):
                sys.exit(f"{checksums}: {parts[1]} has no SHA-256")
            lines[parts[1]] = parts[0]
    if set(lines) != wanted:
        sys.exit(f"{checksums} lacks {sorted(wanted - set(lines))}")
    (setup / "pvault.version").write_text(version + "\n")
    (setup / "pvault-checksums.txt").write_text("".join(f"{lines[n]}  {n}\n" for n in sorted(lines)))


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--library", type=Path, default=os.environ.get("PRATICHE_NAPOLI_DIR"))
    ap.add_argument("--out", type=Path, default=HERE / "dist")
    ap.add_argument("--pvault-version", help="the pvault release the setup skill installs, e.g. 0.1.0 (with --pvault-checksums)")
    ap.add_argument("--pvault-checksums", type=Path, help="that release's checksums.txt")
    ap.add_argument("--connector", help="the Portali connector's https URL (connector/): the plugin brings it")
    a = ap.parse_args()
    if a.connector and not re.fullmatch(r"https://[^\s/]+(/\S*)?", a.connector):
        ap.error("--connector must be an https URL")
    if bool(a.pvault_version) != bool(a.pvault_checksums):
        ap.error("--pvault-version and --pvault-checksums go together")
    if not a.library:
        ap.error("--library or $PRATICHE_NAPOLI_DIR: a pratiche-napoli checkout")
    plugin = a.out / "portali"
    if plugin.exists():
        shutil.rmtree(plugin)
    shutil.copytree(HERE / "portali", plugin, ignore=shutil.ignore_patterns(".DS_Store", "__pycache__"))
    # The licence and notices travel with the plugin: it bundles the library's CC BY-SA texts.
    for name in ("LICENSE", "NOTICE.md"):
        shutil.copy2(REPO / name, plugin / name)
    if a.connector:  # without one the skill uses coverage.json, as of the build
        (plugin / ".mcp.json").write_text(as_json({"mcpServers": {"portali": {"type": "http", "url": a.connector}}}))
    if a.pvault_version:  # without one the setup skill says there is no release to install from
        write_pvault_release(plugin / "skills" / "portali-setup", a.pvault_version, a.pvault_checksums)
    skill = plugin / "skills" / "portali-pratica"
    copy_runner(skill)
    catalog = copy_pratiche(skill, a.library)
    copy_library(a.library, skill)
    manifest = json.loads((plugin / ".claude-plugin" / "plugin.json").read_text())
    coverage = write_coverage(skill, catalog, a.library, manifest["version"])
    make_test_case(plugin / "skills" / "portali-cowork-test")
    self_check(plugin)
    if not re.fullmatch(r"[a-z0-9-]+", manifest["name"]):
        sys.exit(f"plugin name {manifest['name']!r} isn't kebab-case")
    zipped = a.out / f"{manifest['name']}.plugin"
    with zipfile.ZipFile(zipped, "w", zipfile.ZIP_DEFLATED) as z:
        for f in sorted(plugin.rglob("*")):
            if f.is_file() and "__pycache__" not in f.parts and f.name != ".DS_Store":
                z.write(f, f.relative_to(plugin))
    counts = {s: sum(e["status"] == s for e in coverage) for s in ("available", "paused", "mapped", "guide", "coming")}
    print(f"{zipped}: {manifest['name']} {manifest['version']}, {len(catalog)} pratiche, "
          f"{zipped.stat().st_size // 1024} KB; coverage {counts}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
