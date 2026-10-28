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
lacks. One file per form a pratica has used so far.
