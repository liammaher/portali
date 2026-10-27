# Windows: what's built, and what's left

pvault runs on Windows: the CLI and server, Windows Hello in Touch ID's place, and
Credential Manager in the Keychain's. All of it was written and tested on macOS (unit tests,
Windows cross-compiles, `go vet` for windows/amd64, arm64 and 386). **None of it has run on
Windows yet.** This note is the checklist for the first Windows session, and what remains
after it.

## Built

| Phase | What | PR |
|---|---|---|
| W1 | Named pipe instead of the Unix socket (random name in `<vault>\run\pipe`, held open by the server), peer check (token SID + exe path), `POST /vault/stop` for `pvault lock`, owner-only DACLs (`vault.RestrictDir`), detached background server, MCP and `fill.py` over the pipe, Windows CI job and release zips | #72 |
| W2 | Windows Hello approvals: the server shows the prompt (`internal/winhello`, WinRT from pure Go) and issues exactly what it names; `pvault hello`; one approver for Touch ID and Windows Hello in the CLI; the prompt wording shared with the Touch ID helper (`testdata/prompts.json`) | #71, #74 |
| W3 | Credential Manager (`internal/wincred`) for `--keychain`, file-sync sign-ins, `pvault credential read\|write` (MCP token, PEC password); `fill.py` pipe timeout; Windows memory locking; README setup | #77 |

The security notes are in CLAUDE.md (Credentials never travel over TCP, Windows Hello,
Conventions). The decisions: the server asks Windows Hello itself (no helper, no stored
password, because Windows has no per-app Keychain ACL); `pvault unlock` keeps the password;
Windows Hello needs Windows 11; the pipe has a random name, since pipe names are
machine-wide.

## 1. First Windows session (Windows 11; a VM with a Hello PIN works)

Build `pvault.exe` (`go build -o bin\pvault.exe .\cmd\pvault`, or a release zip), put it on
PATH, and use Windows Terminal unless a step says otherwise. Note what happens at each step;
anything unexpected is a bug to file.

**Basics**
1. `pvault init`, then `pvault unlock`. Expect: `%USERPROFILE%\.pvault` exists, and
   `icacls %USERPROFILE%\.pvault` lists only your account; `run\pipe` holds a name like
   `\\.\pipe\pvault-<32 hex>`.
2. `pvault status` shows the server; `pvault set identity.full_name "Test"` asks for the
   password and saves the value.
3. `pvault lock`. Expect: "Vault locked (server stopped)", no `pvault.exe` left in Task
   Manager, `run\pipe` gone. Run `pvault unlock` again.

**Windows Hello: the W0 questions**
4. `pvault hello enable` (asks for the password), then `pvault hello status`: "on".
5. `pvault get identity.full_name`. Expect a Windows Hello prompt **in front of the
   terminal**, reading *pvault wants to read identity.full_name from your vault for 2
   minutes.* Approve: the value prints. Run it again and cancel: it says not approved and
   asks for the password.
6. Repeat 5 from cmd.exe (conhost) and from VS Code's terminal. **If the prompt opens behind
   the terminal or without focus**, fix it in `internal/winhello` or `cmd/pvault/window_windows.go`.
   Options: the CLI calls `AllowSetForegroundWindow` for the server's pid; or, as KeePassXC
   and Bitwarden do, find the window of class `Credential Dialog Xaml Host` and call
   `SetForegroundWindow` on it.
7. The longest prompt: `pvault get 'identity.*' 'residenza.*' 'domicilio.*' 'cie.*'
   'tessera_sanitaria.*' 'abitazione.*' 'locazione.*' 'utenze.*' 'tari.*' 'famiglia.*'`.
   **Check that the whole message is visible**, ending with "for 2 minutes." If Windows cuts
   it off, the prompt would understate the grant. Measure the length that still shows in
   full, then make `approvedGrant` (`internal/api/handlers.go`) refuse a longer message
   with 400, so the CLI falls back to the password; never truncate it.
8. Turn Windows Hello off in Settings (Sign-in options), then `pvault get identity.full_name`:
   it says Windows Hello isn't set up and asks for the password.
9. Sign review: have an agent file a request (MCP `vault_sign_request`), then run
   `pvault sign review <id>`. The prompt names the file, page count, hash and requester; the
   PDF opens.

**Agents and secrets**
10. Register the MCP server as in mcp/README.md, with `VAULT_KEYCHAIN_SERVICE=pvault-mcp` and
    `PVAULT_BIN` set. Run `pvault create-service-token claude --scope "identity.*" --ttl 1h
    --keychain pvault-mcp`. Expect: `cmdkey /list` shows `pvault-mcp`, and the MCP tool
    `vault_get identity.full_name` answers over the pipe.
11. PEC: `pvault credential write pvault-pec --account you@pec.it`, set `PEC_ADDRESS` and
    `PEC_KEYCHAIN_SERVICE=pvault-pec`, then `pec_folders`.
12. `pvault credential read pvault-files-x` is refused (a file source's sign-in).
13. `fill.py` with `--vault-dir %USERPROFILE%\.pvault` and `VAULT_TOKEN`, or the
    `--keychain` name: it reads over the pipe. Also try with the server stopped: it says no
    vault server, it doesn't hang.
14. A file source over OAuth (`pvault files source connect ...`): `cmdkey /list` shows
    `pvault-files-<id>` plus its `.g<gen>.c<n>` chunks, and `files sync` works.

**Windows 10, if available:** `pvault hello enable` refuses (Windows Hello needs Windows 11);
everything else works.

## 2. The test suite on Windows

Run `go test ./...` on Windows (no `-race`: it needs cgo there), in the VM or in the
`windows` CI job. The Unix-only checks are skipped, but expect a few more POSIX assumptions to
surface; fix them with a `runtime.GOOS` skip only where the behaviour truly differs. The
Windows-only tests are `transport_windows_test.go`, `keychain_windows_test.go`,
`perm_windows_test.go`, `credstore_windows_test.go` and `wincred_windows_test.go`.

## 3. Known limits (by design, documented)

- Another program running as you can hold the one Windows Hello prompt slot with a prompt you
  never see, for up to 3 minutes. That only blocks Windows Hello; in a terminal, commands fall
  back to the password.
- Credential Manager has no per-app access list: any program running as you can read it.
- The pipe's peer check matches Linux (same user, same exe path), not macOS's cdhash. Same-user
  code can inject into a running pvault, because Windows has no hardened runtime.
- The release binaries aren't code-signed, so SmartScreen warns the first time.

## 4. Optional

- Tag a release; goreleaser already builds the Windows zips.
- Code-sign the Windows binaries, e.g. Azure Artifact Signing (about $10/month). It's for
  SmartScreen only; nothing in the security design depends on it.
- After the first session, update the `vault` and `pratica` skills with anything Windows
  turned out to need.
