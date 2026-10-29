# Installing

**Status: not released yet.** The pieces below are built and tested offline; nothing is published until
the first release tag (`v0.2.0`) is pushed, so `marketplace add` has nothing to point at today.

## What you do

1. **Add the plugin.** In Claude Code:

   ```
   /plugin install portali --marketplace liammaher/personal-vault-italia
   ```

   (or `claude plugin marketplace add liammaher/personal-vault-italia`, then
   `claude plugin install portali@portali`).
2. **Say "set me up"** if you want the private vault. Claude asks before it downloads anything.
3. **Choose a password and save the secret key** `pvault init` prints, in a terminal. This is the one
   step that is yours alone: neither is ever typed or shown in the conversation.

Pratiche that don't use the vault need no step 2 or 3: the folder-and-`values.json` way works as soon as
the plugin is installed.

## What happens, and what is checked

```
.claude-plugin/marketplace.json  --pinned by SHA-256-->  portali-<version>.zip  (a release asset)
portali-<version>.zip  contains  pvault-checksums.txt  (SHA-256 of each pvault archive of that release)
install-pvault.sh | .ps1  downloads  pvault_<version>_<os>_<arch>  and refuses it unless it matches
```

- The marketplace file is pinned to one release's zip, so Claude Code refuses a zip that isn't the one
  the release published. A pull request moves the pin at each release (`plugin/pin_marketplace.py`).
- The zip carries the checksums of the `pvault` downloads, so a swapped binary is refused even if the
  release page were tampered with. The checksum is not fetched with the download.
- The installer needs no admin rights, edits no PATH or shell profile (Windows: only with
  `-AddToPath`), unpacks one named file from the archive, and runs nothing but that `pvault`, to print
  its version.
- It installs to `~/.local/bin` (macOS, Linux) or `%LOCALAPPDATA%\Programs\pvault` (Windows).
  To remove pvault, delete that file; your vault lives in `~/.pvault` and is not touched.

## What is not done

- **Not signed or notarized.** On macOS the installer ad-hoc signs the binary, as `make build` does. A
  file fetched with `curl` is not quarantined, so Gatekeeper should not block it, and this needs no
  Apple developer account; that is **untried on a clean Mac**. On Windows the script is **untried**, and
  SmartScreen behaviour is unknown.
- **Touch ID** needs the Swift helper, which is built only by `make build`; installed this way you use the
  password.
- **Creating the vault still needs a terminal** (stage two: `pvault setup` in your browser).
- **Running a pratica still needs Python** on your own computer (stage three), except in Cowork, whose
  sandbox has it.
- **A listing in Anthropic's directory** (stage four) would make step 1 a single "Add".

## Releasing (maintainers)

1. Bump `version` in `plugin/portali/.claude-plugin/plugin.json`; the release workflow refuses a tag that
   differs from it.
2. Tag `vX.Y.Z` and push the tag. The workflow runs the tests, builds the six `pvault` archives and
   `checksums.txt` (goreleaser), builds the plugin with them and attaches it as `portali-X.Y.Z.zip`, then
   opens a pull request that pins `.claude-plugin/marketplace.json` to it.
3. Merge that pull request. Until then `marketplace add` finds no marketplace.
4. On a clean Mac and a clean Windows machine: add the marketplace, say "set me up", and check
   `pvault version`. (The installer's tests run against a fake release in CI:
   `plugin/test_install.py`.)
