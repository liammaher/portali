package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/lovincyrus/personal-vault/internal/napoli"
	"github.com/lovincyrus/personal-vault/internal/vault"
)

const locateUsage = `usage: pvault locate [residenza | domicilio | locale | sede_legale]
         an address of the vault; a person's is leo:residenza, a second premises locale2
       pvault locate --street <via> [--civico <n>] [--quartiere <name>]
         an address typed here: reads nothing from the vault, asks for no password`

// exitUnresolved is the exit status when the municipalità can't be worked
// out (1 is an error), so a script can tell the two apart.
const exitUnresolved = 3

func cmdLocate() {
	out, resolved, err := runLocate(os.Args[2:])
	if err != nil {
		fatal("%v", err)
	}
	fmt.Print(out)
	if !resolved {
		os.Exit(exitUnresolved)
	}
}

// runLocate answers pvault locate: the text to print and whether the
// municipalità was found. It only reads: to store an answer (the quartiere
// of a street that crosses a boundary, an override) you run pvault set.
func runLocate(args []string) (string, bool, error) {
	if len(args) > 0 && strings.HasPrefix(args[0], "--") {
		var street, civico, quartiere string
		if err := parseFlags(args, map[string]*string{"--street": &street, "--civico": &civico, "--quartiere": &quartiere}); err != nil {
			return "", false, fmt.Errorf("%v\n%s", err, locateUsage)
		}
		if strings.TrimSpace(street) == "" {
			return "", false, fmt.Errorf("--street is needed\n%s", locateUsage)
		}
		q := napoli.Query{Street: street, Civico: civico, Quartiere: quartiere}
		out, resolved := formatLookup(napoli.Lookup(q), q)
		return out, resolved, nil
	}
	if err := checkArgs(args, 0, 1); err != nil {
		return "", false, fmt.Errorf("%v\n%s", err, locateUsage)
	}
	target := "residenza"
	if len(args) == 1 {
		target = args[0]
	}
	_, ids, ok := vault.LocatedTarget(target)
	if !ok {
		return "", false, fmt.Errorf("%q is not an address\n%s", target, locateUsage)
	}
	// One grant for the three computed fields: the street is not asked for.
	needGrant(strings.Join(ids[:], ","), "read")
	resp, err := apiRequest("GET", "/vault/located/"+target, nil)
	if err != nil {
		return "", false, fmt.Errorf("request failed: %v", err)
	}
	var loc vault.Located
	if err := apiResult(resp, &loc); err != nil {
		return "", false, err
	}
	out, resolved := formatLocated(loc, ids)
	return out, resolved, nil
}

// formatLocated renders what the vault says about an address.
func formatLocated(loc vault.Located, ids [3]string) (string, bool) {
	var b strings.Builder
	mun := loc.Fields[ids[vault.LocMunicipalita]]
	if mun.Value == "" {
		reason := loc.Reason
		if reason == "" {
			reason = "the municipalità can't be worked out"
		}
		fmt.Fprintf(&b, "%s.\n", upperFirst(reason))
		if len(loc.Choices) > 0 {
			b.WriteString("Which quartiere is the address in?\n")
			for _, c := range loc.Choices {
				fmt.Fprintf(&b, "  pvault set %s %q%s\n", ids[vault.LocQuartiere], c, consequence(c))
			}
		} else {
			fmt.Fprintf(&b, "If you know it: pvault set %s <1-10>\n", ids[vault.LocMunicipalita])
		}
		writeNotes(&b, loc.Notes)
		return b.String(), false
	}
	q, d := loc.Fields[ids[vault.LocQuartiere]], loc.Fields[ids[vault.LocDistretto]]
	b.WriteString(summary(mun.Value, q.Value, d.Value))
	if mun.Source == "stored" {
		b.WriteString("  municipalità: stored by you (an override)\n")
	} else {
		fmt.Fprintf(&b, "  municipalità: %s\n", basisText(loc.Basis))
	}
	if loc.Partial {
		b.WriteString("  the street matched on part of its name: check it\n")
	}
	if q.Source == "stored" {
		b.WriteString("  quartiere: stored by you\n")
	}
	if d.Source == "stored" {
		b.WriteString("  distretto: stored by you (an override)\n")
	}
	writeNotes(&b, loc.Notes)
	return b.String(), true
}

// formatLookup renders a lookup of an address typed on the command line.
func formatLookup(res napoli.Result, q napoli.Query) (string, bool) {
	var b strings.Builder
	switch res.Status {
	case napoli.Resolved:
		b.WriteString(summary(fmt.Sprint(res.Municipalita), res.Quartiere, res.Distretto))
		fmt.Fprintf(&b, "  municipalità: %s\n", basisText(string(res.Basis)))
		if res.Partial || len(res.Streets) > 1 {
			fmt.Fprintf(&b, "  matched: %s\n", strings.Join(res.Streets, "; "))
		}
		if res.Partial {
			b.WriteString("  the street matched on part of its name: check it\n")
		}
		return b.String(), true
	case napoli.Ambiguous:
		fmt.Fprintf(&b, "%s.\n", upperFirst(res.Reason))
		if choices := res.QuartiereChoices(); len(choices) > 0 {
			b.WriteString("Which quartiere is the address in?\n")
			for _, c := range choices {
				fmt.Fprintf(&b, "  pvault locate --street %q%s --quartiere %q%s\n", q.Street, civicoFlag(q.Civico), c, consequence(c))
			}
		}
	default:
		fmt.Fprintf(&b, "%s.\n", upperFirst(res.Reason))
	}
	return b.String(), false
}

// summary is the one line: Municipalità 1 · San Ferdinando · ASL Napoli 1 Centro, distretto 24/73.
func summary(mun, quartiere, distretto string) string {
	parts := []string{"Municipalità " + mun}
	if quartiere != "" {
		parts = append(parts, quartiere)
	}
	if distretto != "" {
		parts = append(parts, napoli.ASL+", distretto "+distretto)
	}
	return strings.Join(parts, " · ") + "\n"
}

func basisText(basis string) string {
	switch basis {
	case string(napoli.BasisCivico):
		return "from the civico, by a range in the 2003 stradario; check it if the building was renumbered"
	case string(napoli.BasisQuartiere):
		return "from the quartiere"
	}
	return "from the street (all of it is in this municipalità)"
}

// consequence says where a quartiere leads: "   → municipalità 2, distretto 31".
func consequence(quartiere string) string {
	q, ok := napoli.FindQuartiere(quartiere)
	if !ok {
		return ""
	}
	d, _ := napoli.Distretto(q.Municipalita)
	return fmt.Sprintf("   → municipalità %d, distretto %s", q.Municipalita, d)
}

func civicoFlag(civico string) string {
	if strings.TrimSpace(civico) == "" {
		return ""
	}
	return fmt.Sprintf(" --civico %q", civico)
}

func writeNotes(b *strings.Builder, notes []string) {
	for _, n := range notes {
		fmt.Fprintf(b, "note: %s\n", n)
	}
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
