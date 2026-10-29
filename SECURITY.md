# Security

Portali fills Italian government portals for you, and `pvault`, its encrypted local vault, holds the
details it uses. A flaw here can expose someone's identity data, so please report it privately.

## How to report

Use GitHub's private vulnerability reporting:
[Security tab, "Report a vulnerability"](https://github.com/liammaher/portali/security/advisories/new).
Please don't open a public issue or pull request for a vulnerability, and don't put real personal data in
a report: made-up values show the problem just as well.

This is a free, volunteer-run project. There is no bounty and no guaranteed response time, but every
report is read.

## What counts

- **The vault (`pvault`):** how keys and sessions are handled; a service token or grant reaching more than
  its scope, or any token reaching the `personal` tier; the sign gate approving a file other than the one
  shown; backups or documents readable without the key.
- **Portali's runner and plugin:** any way for the values it fills to reach the conversation (a report, an
  error, a screenshot, the code window's code); the runner declaring, signing, paying or sending
  something it must leave to the person.
- **The release chain:** the installer accepting a download that doesn't match its checksum; the
  marketplace pin or the release files not matching each other.

## What doesn't

- An attacker who already controls your computer or your account. The limits are documented in the
  Security Model of [CLAUDE.md](CLAUDE.md).
- The portals themselves: report those to the office that runs them.
- A Claude session that you let read a page, a screenshot or a file that you were told to keep private.

## Supported versions

The latest release only.

## Something of yours is here

If something in this repository is yours and you want it credited, changed or removed, see
[NOTICE.md](NOTICE.md).
