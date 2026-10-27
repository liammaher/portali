package napoli

import (
	"slices"
	"strconv"
)

// ASL is the one health authority of the whole Comune di Napoli. Its ten
// distretti sanitari follow the ten municipalità, so a municipalità names a
// distretto. Other comuni belong to other ASL (Napoli 2 Nord, Napoli 3 Sud),
// which is a lookup by comune, not by address.
const ASL = "ASL Napoli 1 Centro"

// distretti maps a municipalità to its distretto sanitario, as the ASL lists
// their quartieri at https://www.aslnapoli1centro.it/distretti. 24/73 also
// serves Capri, which is another comune. Checked against the ASL's own list
// of quartieri per distretto (not street by street): where a quartiere is
// split between two distretti the ASL's list would be the source to ask.
var distretti = map[int]string{
	1: "24/73", 2: "31", 3: "29", 4: "33", 5: "27",
	6: "32", 7: "30", 8: "28", 9: "26", 10: "25",
}

// Distretto returns the distretto sanitario number of a municipalità (1-10).
func Distretto(municipalita int) (string, bool) {
	d, ok := distretti[municipalita]
	return d, ok
}

// Municipalita lists the municipalità, 1 to 10, as the vault stores them.
func Municipalita() []string {
	out := make([]string, 0, len(distretti))
	for m := 1; m <= len(distretti); m++ {
		out = append(out, strconv.Itoa(m))
	}
	return out
}

// Distretti lists the distretti sanitari as the vault stores them, sorted.
func Distretti() []string {
	out := make([]string, 0, len(distretti))
	for _, d := range distretti {
		out = append(out, d)
	}
	slices.Sort(out)
	return out
}

// Quartiere is one of the 30 quartieri of the stradario, each in exactly one
// municipalità.
type Quartiere struct {
	Name         string
	Municipalita int
	label        string // as the stradario PDF writes it
	aliases      []string
}

var quartieri = []Quartiere{
	{"Chiaia", 1, "Chiaia", nil},
	{"Posillipo", 1, "Posillipo", nil},
	{"San Ferdinando", 1, "S.Ferdinando", nil},
	{"Avvocata", 2, "Avvocata", nil},
	{"Montecalvario", 2, "Montecalvario", nil},
	{"Mercato", 2, "Mercato", nil},
	{"Pendino", 2, "Pendino", nil},
	{"Porto", 2, "Porto", nil},
	{"San Giuseppe", 2, "S.Giuseppe", nil},
	{"San Carlo all'Arena", 3, "S.Carlo Arena", nil},
	{"Stella", 3, "Stella", nil},
	{"San Lorenzo", 4, "S.Lorenzo", nil},
	{"Vicaria", 4, "Vicaria", nil},
	{"Poggioreale", 4, "Poggioreale", nil},
	{"Zona Industriale", 4, "Zona Industriale", nil},
	{"Vomero", 5, "Vomero", nil},
	{"Arenella", 5, "Arenella", nil},
	{"Ponticelli", 6, "Ponticelli", nil},
	{"Barra", 6, "Barra", nil},
	{"San Giovanni a Teduccio", 6, "S.Giov.a Ted", nil},
	{"Miano", 7, "Miano", nil},
	{"Secondigliano", 7, "Secondigliano", nil},
	{"San Pietro a Patierno", 7, "S.Pietro a Pat.", nil},
	// The municipalità's own list calls it Piscinola-Marianella.
	{"Piscinola", 8, "Piscinola", []string{"Marianella", "Piscinola Marianella"}},
	{"Chiaiano", 8, "Chiaiano", nil},
	{"Scampia", 8, "Scampia", nil},
	{"Soccavo", 9, "Soccavo", nil},
	{"Pianura", 9, "Pianura", nil},
	{"Bagnoli", 10, "Bagnoli", nil},
	{"Fuorigrotta", 10, "Fuorigrotta", nil},
}

// Quartieri lists the 30 quartieri, in municipalità order.
func Quartieri() []Quartiere {
	out := make([]Quartiere, len(quartieri))
	copy(out, quartieri)
	return out
}

var (
	quartiereByLabel = map[string]*Quartiere{}
	quartiereByFold  = map[string]*Quartiere{}
)

func init() {
	for i := range quartieri {
		q := &quartieri[i]
		quartiereByLabel[q.label] = q
		for _, n := range append([]string{q.Name, q.label}, q.aliases...) {
			quartiereByFold[foldName(n)] = q
		}
	}
}

// FindQuartiere reads a quartiere as a person writes it: "S. Carlo all'Arena",
// "san giovanni a teduccio", "Marianella".
func FindQuartiere(s string) (Quartiere, bool) {
	q, ok := quartiereByFold[foldName(s)]
	if !ok {
		return Quartiere{}, false
	}
	return *q, true
}
