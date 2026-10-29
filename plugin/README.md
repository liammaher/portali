# The Portali plugin for Cowork

What an architect in Naples installs in Cowork, on a Pro plan: the portal runner, the
rehearsals' steps files and the pratiche-napoli library, with no server and no vault
(`docs/design/packaging.md`). `portali/` is what's written by hand; `build.py` adds the rest.

```sh
PRATICHE_NAPOLI_DIR=../pratiche-napoli ~/.cache/pratica-portal/venv/bin/python plugin/build.py
```

writes `plugin/dist/portali/` and `plugin/dist/portali.plugin` (install it in Cowork
from a file). The build:

- copies `run.py`, `rules.py`, `inpage.js`, `catalogue.py` (for its drift refusal, with
  `drift.json`), `fill.py`, `review.py` and `schema.json` into the skill unchanged;
- writes each rehearsal's steps file (the generated one first, as `bench/rerun.py` picks it)
  and the rules tables as JSON, which `shim/yaml.py` reads in PyYAML's place: Cowork runs
  the plugin with no packages;
- lists them in `catalog.json` with their library procedure, `supports` refusals and notes;
- copies the library's procedures, rules, offices, situations and `signing.json`;
- makes the test skill's case from the CILA rehearsal's P6 fixture (studio and job split as
  an architect's folder holds them) and a 3 MB test PDF;
- stops unless the test case passes `check.py` and pre-flight under the system `python3`,
  and every pratica's pre-flight prints the same from the plugin as from the repo's runner.

Two skills: `portali-pratica` (the job) and `portali-cowork-test` (the tests that decide the
design: script calls in Cowork's browser, a 3 MB attachment, a run's cost; it writes
`portali-test-results.md` to bring back).

Rebuild after a change to the runner, a steps file, the dictionary or the library, and bump
`version` in `portali/.claude-plugin/plugin.json`.
