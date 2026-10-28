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

How well it drafts (each box checked by hand against the schema and, where there is one, a
hand walk; "blank" is a box it leaves for a person):

| Pratica | Boxes | Right | Blank | Wrong | Note |
|---|---|---|---|---|---|
| acconciatore, apertura | 31 | 31 | 0 | 0 | tuned on it |
| restaurant (somministrazione, zone non tutelate), apertura | 115 | 113 | 1 | 1 | tuned on it; before: 9 right |
| restaurant, subingresso and trasferimento | 47 | 27 | 7 | 13 | not tuned on them; before: 26, 1, 20 |

What it gets wrong is mostly what only the law's words say: a "precedente ubicazione" is the
schema's `_da` (from) fields and the new site its `_a` (to) fields; a form that keeps an
address as one line can't fill the portal's six boxes.
