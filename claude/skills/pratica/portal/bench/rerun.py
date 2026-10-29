#!/usr/bin/env python3
"""Clean reruns of many rehearsals in the Claude browser pane, one after another, nothing
touched between them: the check after a change to the runner, the dictionary or gen.py.

  ~/.cache/pratica-portal/venv/bin/python claude/skills/pratica/portal/bench/rerun.py \\
      --values VALUES.json --out DIR [REHEARSAL ...]

Each rehearsal (rehearsals/<name>: its portal-steps.generated.yaml, else portal-steps.yaml,
and its answers file) goes through run.py's page_files, as `run.py page` would. DIR gets:

  check.js      which of runner.js and steps/<name>.js the pane's storage lacks (byte for
                byte), so only those are sent; run it first, on the portal's origin
  runner.js     inpage.js, as install.js stores it
  steps/*.js    each rehearsal's steps (no personal data)
  queue-N.js    the runs' values, in pieces of at most 40 KB (a message the pane takes in one call)
  loop.js       starts the runs in order and returns at once; each result is kept in the tab
                (sessionStorage pv-batch), so running loop.js again carries on where it stopped
  results.js    the results so far, one line each (`fresh.js` clears them for a new batch)

The pane cuts a call off at 45 s: poll results.js. The values file is what `pvault get`
prints, or a fixture's fields (rehearsals/cila-fittizia/fixture-p6.json "fields" as a dict).
No rehearsal named: every one with a generated steps file and the answers file.
"""

import argparse
import json
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
sys.dont_write_bytecode = True
import run  # noqa: E402
import yaml  # noqa: E402

REHEARSALS = HERE.parents[4] / "rehearsals"
CHUNK = 40_000


def values_of(run_js, proc):
    """The values run.js starts its run with."""
    line = run_js.splitlines()[1]
    head = f"__pv.start({json.dumps(proc)}, "
    assert line.startswith(head) and line.endswith(")"), "run.js changed shape"
    return line[len(head):-1]


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("rehearsals", nargs="*")
    ap.add_argument("--values", required=True, type=Path)
    ap.add_argument("--answers", default="answers-p6.json", help="each rehearsal's answers file")
    ap.add_argument("--out", required=True, type=Path)
    a = ap.parse_args()
    names = a.rehearsals or sorted(d.name for d in REHEARSALS.iterdir()
                                   if (d / "portal-steps.generated.yaml").exists() and (d / a.answers).exists())
    fields = json.loads(a.values.read_text())
    (a.out / "steps").mkdir(parents=True, exist_ok=True)
    stored, sources, origin, queue, failed = {}, {}, None, [], []
    for name in names:
        d = REHEARSALS / name
        steps_file = d / "portal-steps.generated.yaml"
        if not steps_file.exists():
            steps_file = d / "portal-steps.yaml"
        steps = yaml.safe_load(steps_file.read_text())
        values = run.Values(json.loads((d / a.answers).read_text()), fields=fields, asks=steps.get("asks"))
        try:
            files, parts = run.page_bundle(steps, values)
        except run.StepError as e:
            failed.append(f"{name}: {e}")
            continue
        proc = steps["procedure"]
        (a.out / "runner.js").write_text(files["runner.js"])
        (a.out / "steps" / f"{name}.js").write_text(files["steps.js"] + "'installed'\n")
        stored |= parts
        sources |= {"pv-runner": "runner.js", f"pv-steps:{proc}": f"steps/{name}.js"}
        origin = origin or run.re.match(r"https?://[^/]+", steps["logged_in"])[0]
        queue.append(f"[{json.dumps(name)},{json.dumps(proc)},{values_of(files['run.js'], proc)}]")
    if failed:
        sys.exit("rerun.py: not written:\n  " + "\n  ".join(failed))
    (a.out / "check.js").write_text(run.check_script(stored, origin, sources, then="the queue files, then loop.js"))
    chunks, cur = [], []
    for q in queue:
        if cur and sum(map(len, cur)) + len(q) > CHUNK:
            chunks.append(cur)
            cur = []
        cur.append(q)
    chunks.append(cur)
    for i, c in enumerate(chunks):
        (a.out / f"queue-{i}.js").write_text(
            ("window.pvBatch = [];\n" if i == 0 else "")
            + "window.pvBatch = window.pvBatch.concat([" + ",".join(c) + "]);\nwindow.pvBatch.length\n")
    (a.out / "loop.js").write_text("""if (!window.__pv) (0, eval)(localStorage.getItem('pv-runner'));
window.pvBatchRun = (async () => {
  const done = JSON.parse(sessionStorage.getItem("pv-batch") || "[]");
  for (const [name, proc, values] of window.pvBatch || []) {
    if (done.some((d) => d.name === name)) continue;
    __pv.start(proc, values);
    let r; do { r = await __pv.wait(30000); } while (r.result === "running");
    done.push({ name, result: r.result, error: r.error || null, at: r.at, sections: r.sections.length,
      verify_seconds: r.verify_seconds ?? null, total_seconds: r.total_seconds, url: r.url });
    sessionStorage.setItem("pv-batch", JSON.stringify(done));
  }
  return "done";
})();
`started: ${(window.pvBatch || []).length} queued`
""")
    (a.out / "results.js").write_text("""await new Promise((r) => setTimeout(r, 40000));
const done = JSON.parse(sessionStorage.getItem("pv-batch") || "[]");
JSON.stringify({ done: done.length, of: (window.pvBatch || []).length,
  filled: done.filter((d) => d.result === "filled").length,
  stopped: done.filter((d) => d.result !== "filled").map((d) => [d.name, d.error, d.at && d.at.section]) })
""")
    (a.out / "fresh.js").write_text('sessionStorage.removeItem("pv-batch"); "cleared"\n')
    print(f"{a.out}: {len(names)} rehearsals, {len(chunks)} queue file(s). In the pane, on {origin}: "
          "check.js, then the files it names, then the queue files, loop.js, and results.js until done.")


if __name__ == "__main__":
    main()
