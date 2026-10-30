# Contribuire / Contributing

Portali resta aggiornato grazie a chi lo usa: nulla è ospitato, quindi richieste e segnalazioni
arrivano come issue e pull request. Prima, leggi l'[uso responsabile](docs/responsible-use.md).

Portali stays current through the people who use it: nothing is hosted, so requests and reports come in
as issues and pull requests. First, read [responsible use](docs/responsible-use.md).

## Senza scrivere codice / Without writing code

- **Chiedi una pratica / Request a pratica:** the
  [issue form](https://github.com/liammaher/portali/issues/new/choose). How often you file it and how
  long it takes decide what is mapped next ([north star](docs/north-star.md#what-to-map-next)).
- **Il portale è cambiato / The portal changed:** when a run stops on a section that is no longer as
  mapped, the same page has a form for it. Structure only, meaning the section and the fields' labels
  and ids from `run/report.json` (`error`, `at`, `snapshot`) or `__pv.look()`, never a value.
- **Un errore su cosa chiede la legge / A mistake in what the law asks:** documents, costs, deadlines
  and offices live in [pratiche-napoli](https://github.com/liammaher/pratiche-napoli), with sources.
  Correct them there.
- **Sicurezza / Security:** privately, as [SECURITY.md](SECURITY.md) says.

## Changing a map or the code

**Maps are data.** A pratica's steps file is generated, not written by hand: `gen.py` joins a crawl of
the portal (`claude/skills/pratica/portal/catalogue/<comune>/sections/`), the shared dictionary
(`dictionary/fnsuap.yaml`, each section's fields by label) and the pratica's recipe
(`rehearsals/<name>/recipe.yaml`). A changed section is usually fixed by changing a dictionary entry or a
recipe and regenerating. The rules tables in `claude/skills/pratica/portal/rules/` are copies of the
library's, byte for byte: change the library first. How to add a pratica is in
[the runner's README](claude/skills/pratica/portal/README.md).

**Made-up data only.** Never map, test or rehearse with a real person's or business's data, and never
with a real pratica. Use the fixtures (`rehearsals/cila-fittizia/fixture-p6.json` and the rehearsals'
`answers-*.json`). Name scratch drafts "PROVA NON INVIARE - ..." and never send them. Delete them when
you're done.

**What never changes.** The runner never ticks a declaration, pays, signs or sends, and a case it can't
fill all the way is refused before anything opens. A pratica is called `available` only after it has
filled on the real portal, recorded in its rehearsal's README.

**Tests** (CI runs the first two on every pull request, on Linux, macOS and Windows):

```sh
go test -race ./...                                        # the vault
cd mcp && npx tsc -p tsconfig.test.json && npm test        # the MCP server
cd connector && npx tsc --noEmit -p . && npm test          # the connector
```

The portal runner's tests run offline, with the pratica venv's Python (Playwright's Chromium, headless;
no network, vault or model):

```sh
PY=~/.cache/pratica-portal/venv/bin/python
$PY claude/skills/pratica/portal/bench/test_inpage.py      # inpage.js, on a fake portal
$PY claude/skills/pratica/portal/bench/test_preflight.py
$PY claude/skills/pratica/portal/bench/test_gen.py
PRATICHE_NAPOLI_DIR=../pratiche-napoli $PY claude/skills/pratica/portal/bench/test_rules.py
```

After a change to `inpage.js`, the dictionary or `gen.py`, regenerate every rehearsal and read the diff:
a steps file that changes for no reason is a regression. If you can log in to the portal, rerun them
(`bench/rerun.py`). After a change to anything the plugin carries, rebuild it with `plugin/build.py` and
bump its `version` (see [docs/install.md](docs/install.md)).

**Style.** The conventions are in [CLAUDE.md](CLAUDE.md): a comment says why, CLAUDE.md says what must
stay true, and both stay short. `main` is protected: open a pull request.

**Licence.** Contributions here are under the [MIT licence](LICENSE). The library's procedure texts are
CC BY-SA 4.0 in its own repository.
