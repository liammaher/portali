# Closing a bar or restaurant: generated from the batch crawl, run to the signing screen

"Cessare l'attività" for a bar or restaurant outside the protected zones on Napoli's SUAP (the
closure, and the end of its Reg. 852/2004 registration), for the pizzeria rehearsal's made-up
ditta individuale. The draft is "PROVA NON INVIARE - cessazione test firma". Its two own
sections (which activity closes and from when; the 852 registration it ends) were written
into the dictionary by hand.

## Runs

| Date | Browser | Result |
|---|---|---|
| 2026-10-28 | pane | Stopped once in the wizard: this pratica has no "interventi eventuali" screen, which the batch crawl had recorded and the recipe didn't use (`gen.py` now drops a recipe's wizard step for a screen the crawl didn't see). Resumed on the draft: 51 values, every one read back on a fresh load. |

## The signing screen (a test of whether the portal shows the pratica's XML before signing)

Liam allowed the declarations to be ticked on this test draft only. With them ticked and the
diritti set to "non dovuti", the draft reaches "Firma digitale del modello di attività": it
offers one file, `<CF>-<date>-<n>.001.MDA.PDF` (the modello di attività, 155 KB), to download,
sign and upload again, or "Firma On-Line". It is a plain PDF/A made with iText: no embedded
files, no XML, no form fields. The pratica's XML (`.SUAP.xml`, `.MDA.xml`) is built only when
it is sent, so the national field names can't be read from the portal before sending. Nothing
was signed, paid or sent; the draft is Liam's to delete.

What the declarations needed, for the dictionary's notes:

- "Dichiarazione in materia di rispetto sulle tasse comunali": the usual answer is the first
  choice with its two boxes (no debts with the Comune, no forced collection under way). "Non
  rientrare nei due casi precedenti" asks for a written reason.
- "Requisiti professionali" (the restaurant): choosing a requisite opens its details (for a
  course: the institute, its town, the course, the year).
- On the restaurant, the planimetria and the ASL tariff receipt become required on the
  attachments screen once the declarations are done: both signed-only, so the person's.
