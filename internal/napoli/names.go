package napoli

import (
	"regexp"
	"slices"
	"strings"
)

var accents = strings.NewReplacer("À", "A", "È", "E", "É", "E", "Ì", "I", "Ò", "O", "Ù", "U", "Ç", "C")

// abbreviations people write for a street type; the stradario spells them out.
var abbreviations = []struct {
	re   *regexp.Regexp
	long string
}{
	{regexp.MustCompile(`\bC\.SO\b`), "CORSO"},
	{regexp.MustCompile(`\bV\.LE\b`), "VIALE"},
	{regexp.MustCompile(`\bP\.?ZZ?A\b`), "PIAZZA"},
	{regexp.MustCompile(`\bP\.?TTA\b`), "PIAZZETTA"},
	{regexp.MustCompile(`\bSTR\.`), "STRADA"},
}

var notAlnum = regexp.MustCompile(`[^A-Z0-9]+`)

// articles and prepositions carry no identity ("Via dei Tribunali" and "Via
// Tribunali" are one street); numbers like 1° or 2^ keep their digit.
var stopWords = map[string]bool{
	"DI": true, "DE": true, "DEL": true, "DELLA": true, "DELLE": true, "DEI": true,
	"DEGLI": true, "DELL": true, "DELLO": true, "DA": true, "A": true, "AL": true,
	"ALL": true, "ALLA": true, "ALLO": true, "ALLE": true, "AI": true, "AGLI": true,
	"E": true, "IL": true, "LA": true, "LE": true, "LO": true, "L": true, "D": true,
}

var synonyms = map[string]string{
	"SAN": "S", "SANTA": "S", "SANTO": "S", "SANT": "S", "SS": "S",
	"VICOLO": "VICO",
}

// nameTokens reduces a street name to its identifying words, sorted: word
// order is free because the stradario writes "TASSO TORQUATO" where people
// write "Torquato Tasso".
func nameTokens(s string) []string {
	s = accents.Replace(strings.ToUpper(s))
	for _, a := range abbreviations {
		s = a.re.ReplaceAllString(s, a.long)
	}
	var out []string
	for _, w := range strings.Fields(notAlnum.ReplaceAllString(s, " ")) {
		if stopWords[w] {
			continue
		}
		if r, ok := synonyms[w]; ok {
			w = r
		}
		out = append(out, w)
	}
	slices.Sort(out)
	return out
}

// foldName is nameTokens as one comparable string, for quartieri.
func foldName(s string) string { return strings.Join(nameTokens(s), " ") }

// containsAll reports whether every token of sub is in tokens (both sorted),
// counting repeats.
func containsAll(tokens, sub []string) bool {
	i := 0
	for _, t := range sub {
		for i < len(tokens) && tokens[i] < t {
			i++
		}
		if i == len(tokens) || tokens[i] != t {
			return false
		}
		i++
	}
	return true
}
