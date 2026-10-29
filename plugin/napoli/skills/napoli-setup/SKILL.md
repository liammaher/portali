---
name: napoli-setup
description: Sets up the private vault (pvault) on this computer: installs the program from the project's release, checked against a checksum this plugin carries, and helps the person create their vault. Use when the user asks to set up, install or update the vault or pvault, or when a pratica needs their saved details and `pvault` isn't installed.
---

# Setting up pvault

pvault is the encrypted vault that keeps a person's details on their own computer, so a
portal can be filled from it without the details passing through this conversation. This
skill gets the program onto the computer and a vault created. It needs a shell on the
person's own computer (the Code tab, a terminal); in a sandbox with no access to their
computer (Cowork's, for one) say so in one line and stop: the folder-and-`values.json` way in
`napoli-pratica` is what that setup can do.

## 1. Is it already there?

Run `pvault version`. If the command isn't found, try `~/.local/bin/pvault version`
(macOS, Linux) or `%LOCALAPPDATA%\Programs\pvault\pvault.exe version` (Windows). It prints
`pvault <version>`. If that is the version in `pvault.version` next to this file, go to step 3.
A `dev` version is a build from source: leave it alone and go to step 3.

## 2. Install it, with the person's yes

A download needs their explicit yes. Ask once, saying what it is:

> I'll download pvault <version> for <macOS or Windows or Linux>, <chip>, from
> github.com/liammaher/personal-vault-italia (its releases page), a single file of some tens
> of MB. I check it against a checksum that came with this plugin before anything is
> installed, then put it in your user folder (`~/.local/bin`, or `%LOCALAPPDATA%\Programs\pvault`).
> No admin rights, and nothing else on the computer changes. OK?

On yes, run the script that sits next to this file, as it is:

- macOS, Linux: `sh install-pvault.sh`
- Windows: `powershell -NoProfile -ExecutionPolicy Bypass -File install-pvault.ps1`

Its last line is `PVAULT_PATH=<where it is>`. Use that path from here on if `pvault` isn't on
the PATH; don't edit the PATH or a shell profile unless the person asks (Windows: the script's
`-AddToPath` does it for new terminals).

If it says `checksum mismatch`, **stop**: tell the person plainly that the download didn't
match and nothing was installed. Don't retry from another place, don't skip the check, don't
download by hand. If it says there is no release to install from, this copy of the plugin was
built from source: say so; building pvault (`make build`) is the developer's way.

## 3. The vault

Run `pvault status`. If it says the vault isn't initialized, the person creates it themselves:
the profile password and the secret key it prints are theirs alone.

- Open a terminal for them if you have a way to (the app's Terminal panel), or tell them to open
  Terminal (macOS) or PowerShell (Windows).
- They run `pvault init` there: they choose the password and **save the secret key it shows**
  (without both, the vault can't be opened). Tell them that before they start.
- **Never** ask them to type or paste the password or the key into this conversation, never run
  `pvault init` or `pvault unlock` for them, and never read the key.

Then `pvault unlock` (they type the password) and `pvault ui` opens the page where they enter
their details: you see nothing of it. When they say they're done, `pvault list` shows which
fields exist (not their values).

## 4. Say what's next

One line: what a pratica will now use from the vault, and that you'll only ever see which
fields exist, not what is in them.
