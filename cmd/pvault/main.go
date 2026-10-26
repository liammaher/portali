package main

import (
	"fmt"
	"os"
)

func main() {
	args, err := selectVault(os.Args)
	if err != nil {
		fatal("%v", err)
	}
	os.Args = args
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		cmdInit()
	case "unlock":
		cmdUnlock()
	case "lock":
		cmdLock()
	case "serve":
		cmdServe()
	case "status":
		cmdStatus()
	case "schema":
		cmdSchema()
	case "set":
		cmdSet()
	case "get":
		cmdGet()
	case "list":
		cmdList()
	case "delete":
		cmdDelete()
	case "set-sensitivity":
		cmdSetSensitivity()
	case "export":
		cmdExport()
	case "audit":
		cmdAudit()
	case "backup":
		cmdBackup()
	case "restore":
		cmdRestore()
	case "create-service-token":
		cmdCreateServiceToken()
	case "list-service-tokens":
		cmdListServiceTokens()
	case "revoke-service-token":
		cmdRevokeServiceToken()
	case "doc":
		cmdDoc()
	case "person", "member": // member is the older name, kept as an alias
		cmdPerson()
	case "expiring":
		cmdExpiring()
	case "retention":
		cmdRetention()
	case "onboard":
		cmdOnboard()
	case "ui":
		cmdUI()
	case "touchid":
		cmdTouchID()
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`pvault — Personal Context Protocol vault

Usage: pvault [--vault <name>] <command> [args]

  --vault <name> (or PVAULT_VAULT) picks another vault, e.g. a business vault:
  ~/.pvault-<name>, with its own server, port and Touch ID. Without it: ~/.pvault.

Commands:
  onboard                          Create vault, unlock, and populate common fields
  init [--impresa]                 Create a new vault; --impresa makes a business vault, whose own data
                                   is the business's and whose people each have a ruolo
  unlock                           Unlock vault (starts background server)
  lock                             Lock vault (stops server)
  serve                            Run server in foreground
  status                           Show vault status
  schema                           Show recommended field names (--json for raw JSON)
  set <id> [value]                 Set a field; leave out the value to type it hidden (for secrets).
                                   Another person's fields are <slug>:<id>, e.g. leo:cie.numero
  get <id> [<id>...]               Get field values (up to 10; several print as JSON)
  list [category | slug: | slug:category]
                                   List fields (yours, or another person's)
  delete <id>                      Delete a field
  set-sensitivity <id> <tier>      Set field sensitivity (public|standard|sensitive|critical|personal)
  export [--include-personal]      Export decrypted fields as JSON (personal fields left out by default)
  audit                            Show access audit log
  backup [--to <folder>]           Write an encrypted backup now; --to also sets the folder for
                                   automatic backups (a minute after each change, and on lock)
  restore <file> [--check] [--ask-key]
                                   Recreate the vault from a backup (needs password + secret key);
                                   --check only verifies it, --ask-key types the key to test your copy
  expiring [days]                  Show documents and pratiche deadlines expired or due within days (default 90);
                                   in a business vault also people past retention and clients' ID scans past their period
  retention [set <years> | unset]  How long people's data is kept after their ruolo.data_fine (no default)
  retention scans <days> | unset   How long clients' ID scans are kept after their pratiche close (no default)
  doc add <category> <file> [--sensitivity tier]
                                   Encrypt and store a document scan (max 20 MB)
  doc list [category]              List stored documents
  doc get <id> [--out path]        Decrypt a document to a file (never overwrites)
  doc delete <id>                  Delete a stored document
  person list                      List the other people in your vault: family members
                                   (relazione, age, stato di famiglia), or a business's people (ruolo)
  person show <slug>               Show a person's fields
  person add <slug> --relazione <r> [--nome --cognome --codice-fiscale --data-nascita --sesso]
                                   Add a family member (spouse, child, parent...)
  person add <slug> --ruolo <r> [--nome --cognome --denominazione ...]
                                   In a business vault: add its titolare, a socio, an employee, a client...
  person remove <slug>             Delete a person's fields and documents
  person export <slug> [--to <folder>] [--include-personal]
                                   Write a person's fields and documents to a new, unencrypted folder
                                   <slug>-export-<date> (data portability, GDPR art. 20)
  person migrate [--dry-run]       Turn old nucleo_familiare.membro<N> fields into people
                                   (pvault member ... is an alias for pvault person ...)
  ui                               Open vault onboarding form in browser
  touchid enable|disable|status    Approve requests with Touch ID instead of typing the password
  create-service-token <consumer> --scope <patterns> [--access read|append|write]
                       [--ttl 24h] [--max-sensitivity public|standard|sensitive|critical] [--keychain <service>]
                                   Create a scoped agent token (read-only by default; 24h default, 90d max;
                                   --keychain saves it to the macOS Keychain instead of printing it)
  list-service-tokens              List active service tokens
  revoke-service-token <prefix>    Revoke a service token by listed prefix or full token`)
}
