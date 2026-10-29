# Notices

## This project

MIT, see [LICENSE](LICENSE). It is a fork of
[lovincyrus/personal-vault](https://github.com/lovincyrus/personal-vault), © 2026 Cyrus Goh, whose
notice stays in `LICENSE`; the additions are © 2026 Liam Maher and contributors.

## Not official

This project is not affiliated with, endorsed by or sponsored by the Comune di Napoli, InfoCamere,
Unioncamere, the Agenzia delle Entrate, INPS or any other ente. Their names identify the services the
software works with. It prepares paperwork and stops before the steps the law keeps for the person; it is
not legal advice. Check the official source before acting.

## Third-party material in this repository

What is here that others published, where it came from, and what is known about reusing it
(checked 2026-10-29). "Not checked" means nobody has read the terms yet, not that they allow or forbid it.

| What | Where | Source | Terms |
|---|---|---|---|
| The national forms' structure: each field's path, type, whether it repeats or is optional, and a note | `claude/skills/pratica/portal/schema/*.json`, `rehearsals/cila-fittizia/schema/*.json` | Flattened by `rehearsals/cila-fittizia/schema/flatten.py` from the XSD files InfoCamere publishes at [InfoCamereSCpA/Sistema-Informatico-degli-Sportelli-Unici-Moduli-PA](https://github.com/InfoCamereSCpA/Sistema-Informatico-degli-Sportelli-Unici-Moduli-PA) (commit `fc13e08`, 12 March 2026) | That repository has no licence file. Credited to InfoCamere; no other part of their files is copied |
| Naples streets with their municipalità and quartiere | `internal/napoli/data/stradario.json` | Built by `internal/napoli/gen/gen.py` from the Comune di Napoli's *stradario per municipalità* (update of 22/12/2025, [comune.napoli.it/stradario](https://www.comune.napoli.it/stradario)) and its *stradario COSAP* (2003, [trasparenza.comune.napoli.it](https://trasparenza.comune.napoli.it/flex/files/1/f/1/D.f5e6878d6c0cf0699c29/6__stradario.pdf)) | Published by the Comune on its website; its reuse terms: not checked |
| What the impresainungiorno portal offers a comune: wizard screens, sections, field labels and options | `claude/skills/pratica/portal/catalogue/`, `rehearsals/*/portal-map.yaml`, `rehearsals/*/portal-steps.yaml` | Crawled by program from [suap.impresainungiorno.gov.it](https://suap.impresainungiorno.gov.it) with the author's own SPID login and made-up data; no personal data is in them | The portal's terms on automated access: not checked. The runner never ticks a declaration, pays, signs or sends |
| The pratiche-napoli library of procedures, rules and offices | Not in this repository. The plugin release (`portali-<version>.zip`) bundles a copy | [liammaher/pratiche-napoli](https://github.com/liammaher/pratiche-napoli): code MIT; procedure texts [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/) | Credit "pratiche-napoli" with a link to that repository, and share adapted procedure texts under the same licence. The plugin carries the library's `LICENSE` and `LICENSE-CONTENT.md` |
| Libraries the code uses | `go.mod`, `mcp/package.json`, `connector/package.json` | Their authors | Each under its own licence, which its package carries |

## If you hold rights in any of this

If something here is yours and you want it credited, changed or removed, open an issue on the repository.
