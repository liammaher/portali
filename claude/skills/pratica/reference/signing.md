# Signing through the vault

A handwritten signature can be stamped by the vault once the user has reviewed that exact
PDF and approved it in person. You can ask for it; you can never approve it or read the
signed file (there is no tool for either, and a token can't).

First, does the form need a signature at all? `fill.py route --channel <type>` prints how the
route the user picked signs (the entry's preferred one unless they chose another): the form's
own `signing` for it, else the law's rule for that channel type (the library's
`contract/signing.json`: PEC and email take a firma digitale or a hand-signed scan with the
ID copy, post and desk a handwritten signature with the ID copy):

- `login` or `none`: no signature step. Say why (the SPID/CIE login, or the PEC from their own
  domicilio digitale, identifies them). Don't ask for a signature.
- `digital` only: a firma digitale is the user's. Hand it over (SKILL.md step 5).
- `handwritten`, and the layout has `signatures`: the gate can carry it (the step it
  carries is the one with `gate: sign`). If `status` is `disputed`, sign by hand: `fill.py
  --sign` refuses it. If it is `unverified`, say the Comune hasn't confirmed that way (and, for `id_copy: required`, that
  the ID copy goes with it), and let the user choose: the gate, or signing by hand as usual.
- Nothing for the channel (a portal or app with no `signing`): follow the portal; never
  guess from another comune.

To use the gate, the user needs a signature image stored once: `pvault doc add
firma_autografa ~/Desktop/signature.png`, a PNG with a transparent background (a white one
would cover the signature line), up to 500 KB, kept as `personal` so no token reads it. You
can't check it; the request says if it's missing. Then:

1. Run `needs` as in SKILL.md "Filling with fill.py" but without `--draft`: its `prepare` token also
   carries `firma_autografa.signing` when the layout has a signature box. Give the user that
   command as before.
2. Run `fill.py run ... --sign` (not a draft: it opens the pratica, once). When every check
   passes it prints `signing request <id> is waiting for the user` with the command, and
   notes how to find it in the pratica's `prossimo_passo` and `report.json`. A "no signature
   on file" or "library lists no handwritten signature" failure means the gate isn't
   available: say which. For a missing signature, give them the `pvault doc add` above and
   run `--sign` again (the open pratica isn't opened twice, only the request is filed); for
   the library, fall back to hand-signing.
3. In one message, give the user the printed `pvault sign review <id>` to run in their
   terminal, in the topic folder: it saves `<name>-firmato.pdf` in the current folder, or
   where `--out <file>` says (a file path, never an existing one). It opens the signed PDF;
   they check it, approve with their password or Touch ID, and type the first 8 characters
   of its hash. Nothing is signed otherwise. The request expires after 30 minutes and with
   the day (the date beside the signature is today's): run `fill.py ... --sign` again
   after that. The user's `pvault sign list` shows it; you see the saved file. `vault_sign_status`
   only sees requests of its own token, not `fill.py`'s.
4. Once approved, list the files to send: the signed PDF and, if the route's `id_copy` is
   `required` (or `handwritten`), a copy of the ID document (they have it in the vault: `vault_documents`, or
   their folder). Sending is theirs (SKILL.md step 5).

For a PDF filled another way (the pdf skill, or by hand), `vault_sign_request` files the same
request with the layout's `signatures` rects. It needs the MCP server's own token to carry
`firma_autografa.signing` with append access, and the PDF inside the MCP import folders
(`VAULT_IMPORT_DIRS`, default `~/Downloads`): ask the user to copy it there. Prefer `fill.py`.
