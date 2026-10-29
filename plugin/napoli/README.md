# Napoli

Prepares a Comune di Napoli pratica on impresainungiorno (SUE and SUAP) from your client's
documents, and stops where your part begins: the declarations, your firma digitale, the
payment and sending. Free; it works on a Claude Pro plan in Cowork.

## Install

In Claude Code, one command adds the marketplace and installs the plugin:

```
/plugin install napoli --marketplace liammaher/personal-vault-italia
```

Then say "set me up" if you want the private vault (`napoli-setup`), or just ask for a pratica.
See `docs/install.md` in the repository for what is checked and what is not.

## Use it

1. Open a folder in Cowork, with one subfolder per job holding the client's documents
   (visura, documento d'identità, elaborati, relazione).
2. Write one line: "CILA per il cliente in Bianchi/, lavori interni, via ...".
3. Answer the one message of questions Claude sends, log in with SPID in Cowork's own
   browser, and read the review page when the draft is ready.

The first time, Claude asks for your studio's details once and keeps them in `studio.json`.

## Where your clients' data goes

Into your folder and your Claude conversation, and onto the portal. Nowhere else: this
plugin has no server. You are the controller of your clients' data: turn off "Help improve
Claude" in Settings → Privacy, and use Cowork's built-in browser, not Claude in Chrome.

## What's in it

- The runner that fills a whole pratica on the portal and reads every value back.
- The pratiche-napoli library: what Naples's offices ask for each procedure, with sources.
- The cases each pratica doesn't cover yet: those are refused before anything opens.

Claude never ticks a declaration, signs, pays or sends.
