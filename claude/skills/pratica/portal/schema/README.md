# National forms, flattened

The national standard forms (moduli unificati) that impresainungiorno's sections are built
from, as InfoCamere publishes them (repository `InfoCamereSCpA/Sistema-Informatico-degli-Sportelli-Unici-Moduli-PA`,
commit `fc13e08`, 12 March 2026), flattened to their leaf fields by
`rehearsals/cila-fittizia/schema/flatten.py`:

```sh
git clone --depth 1 https://github.com/InfoCamereSCpA/Sistema-Informatico-degli-Sportelli-Unici-Moduli-PA modulipa
python3 -I rehearsals/cila-fittizia/schema/flatten.py modulipa/Moduli_PA/SUAP/04_forms/<form>.xsd > <form>-fields.json
```

`draft.py` reads them to draft dictionary entries for a crawled section the dictionary
lacks, several at once for a pratica that joins forms (a restaurant's SCIA and the Reg. 852
notifica). One file per form a pratica has used so far.

How well it drafts (each box against an answer key written before scoring: the right schema
leaf, or none for the portal's own boxes; the second radio of a yes/no pair counts with the
first). The drafter pairs boxes with leaves by order and type, since the portal draws a
section in the schema's order; shared words only repair what the portal's layout moves (Cognome
before Nome, a two-column list); the portal's address block is matched by role and side
(`_da` for a "precedente" section). Each section is marked `exact` or `check`.

| Pratica | Boxes | Order and type (now) | Words first (before) | Note |
|---|---|---|---|---|
| acconciatore, apertura | 31 | 31 | 31 | the words-first drafter was tuned on it |
| restaurant (somministrazione, zone non tutelate), apertura | 115 | 111 | 113 | the words-first drafter was tuned on it |
| restaurant, subingresso and trasferimento | 49 | 44 | 33 | untuned: the fair test |
| non-food shop (vicinato), apertura | 39 | 19 of 25 drafted | | untuned; the 14 left were Napoli's own boxes |

Of the 5 the fair test misses, 2 are arguably right (a box for "name or company" took the
company leaves) and 3 are portal-only boxes paired with a leaf; sections marked `exact` held 17
of 18 boxes right.

What it still gets wrong is the portal's own boxes (Napoli's rules, a web address, an "altro")
that sit where a schema leaf could fit: those are the `check` sections' work for a person.
