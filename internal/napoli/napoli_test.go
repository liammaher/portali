package napoli

import (
	"slices"
	"strings"
	"testing"
)

func TestDataIsConsistent(t *testing.T) {
	streets := load()
	if len(streets) < 3500 {
		t.Fatalf("only %d streets embedded", len(streets))
	}
	muns := map[int]bool{}
	for _, s := range streets {
		if len(s.tokens) == 0 || len(s.segments) == 0 {
			t.Errorf("%q has no tokens or no segments", s.name)
		}
		have := map[string]bool{}
		for _, sg := range s.segments {
			muns[sg.mun] = true
			have[sg.quartiere] = true
			if sg.quartiere == "" {
				continue // the stradario gives a municipalità but no quartiere
			}
			q, ok := quartiereByLabel[sg.quartiere]
			if !ok {
				t.Errorf("%q: unknown quartiere %q", s.name, sg.quartiere)
			} else if q.Municipalita != sg.mun {
				t.Errorf("%q: %s is in municipalità %d, the street says %d", s.name, sg.quartiere, q.Municipalita, sg.mun)
			}
		}
		for q := range s.ranges {
			if !have[q] {
				t.Errorf("%q: civico range for %q, which the street does not cross", s.name, q)
			}
		}
	}
	for m := 1; m <= 10; m++ {
		if !muns[m] {
			t.Errorf("no street in municipalità %d", m)
		}
		if d, ok := Distretto(m); !ok || d == "" {
			t.Errorf("no distretto for municipalità %d", m)
		}
	}
	if _, ok := Distretto(0); ok {
		t.Error("municipalità 0 (area portuale) has no distretto")
	}
	if _, ok := Distretto(11); ok {
		t.Error("there is no municipalità 11")
	}
}

func TestDistrettiAreDistinct(t *testing.T) {
	seen := map[string]int{}
	for m := 1; m <= 10; m++ {
		d, _ := Distretto(m)
		if other, dup := seen[d]; dup {
			t.Errorf("municipalità %d and %d share distretto %s", other, m, d)
		}
		seen[d] = m
	}
}

func TestQuartieri(t *testing.T) {
	qs := Quartieri()
	if len(qs) != 30 {
		t.Fatalf("%d quartieri, want 30", len(qs))
	}
	perMun := map[int]int{}
	for _, q := range qs {
		perMun[q.Municipalita]++
	}
	want := map[int]int{1: 3, 2: 6, 3: 2, 4: 4, 5: 2, 6: 3, 7: 3, 8: 3, 9: 2, 10: 2}
	for m, n := range want {
		if perMun[m] != n {
			t.Errorf("municipalità %d has %d quartieri, want %d", m, perMun[m], n)
		}
	}
	for in, want := range map[string]string{
		"S. Carlo all'Arena":      "San Carlo all'Arena",
		"san carlo all’arena":     "San Carlo all'Arena",
		"S.Carlo Arena":           "San Carlo all'Arena",
		"San Giovanni a Teduccio": "San Giovanni a Teduccio",
		"S.Giov.a Ted":            "San Giovanni a Teduccio",
		"Marianella":              "Piscinola",
		"Piscinola-Marianella":    "Piscinola",
		"S.Pietro a Patierno":     "San Pietro a Patierno",
		"chiaia":                  "Chiaia",
		"Zona Industriale":        "Zona Industriale",
	} {
		q, ok := FindQuartiere(in)
		if !ok || q.Name != want {
			t.Errorf("FindQuartiere(%q) = %q, %v; want %q", in, q.Name, ok, want)
		}
	}
	if _, ok := FindQuartiere("Colli Aminei"); ok {
		t.Error("Colli Aminei is not a quartiere of the stradario")
	}
}

func TestInNapoli(t *testing.T) {
	for in, want := range map[string]bool{"Napoli": true, " NAPOLI ": true, "napoli": true, "Pozzuoli": false, "Casoria": false, "": false} {
		if got := InNapoli(in); got != want {
			t.Errorf("InNapoli(%q) = %v", in, got)
		}
	}
}

// Every street must be found by its own stradario name, exactly.
func TestEveryStreetFindsItself(t *testing.T) {
	for _, s := range load() {
		r := Lookup(Query{Street: s.name})
		if r.Status == NotFound || r.Partial || !slices.Contains(r.Streets, s.name) {
			t.Errorf("%q: %+v", s.name, r)
		}
	}
}

func TestLookup(t *testing.T) {
	type want struct {
		status    Status
		mun       int
		quartiere string
		basis     Basis
		distretto string
	}
	resolved := func(mun int, quartiere string, basis Basis, distretto string) want {
		return want{Resolved, mun, quartiere, basis, distretto}
	}
	for _, c := range []struct {
		name string
		q    Query
		want want
	}{
		// A street inside one municipalità.
		{"plain", Query{Street: "Via Alabardieri"}, resolved(1, "Chiaia", BasisStreet, "24/73")},
		{"piazza", Query{Street: "Piazza Vanvitelli"}, resolved(5, "Vomero", BasisStreet, "27")},
		{"lower case and spaces", Query{Street: "  via   alabardieri "}, resolved(1, "Chiaia", BasisStreet, "24/73")},
		{"surname first in the stradario", Query{Street: "Via Tiberio Cavallo"}, resolved(7, "Secondigliano", BasisStreet, "30")},
		{"as written in the stradario", Query{Street: "VIA CAVALLO TIBERIO"}, resolved(7, "Secondigliano", BasisStreet, "30")},
		{"article left out", Query{Street: "Via Tribunali", Civico: "376"}, resolved(2, "San Giuseppe", BasisCivico, "31")},
		{"abbreviated type", Query{Street: "C.so Vittorio Emanuele", Civico: "453"}, resolved(2, "Avvocata", BasisCivico, "31")},
		{"saint written out", Query{Street: "Via San Giacomo dei Capri"}, resolved(5, "Arenella", BasisStreet, "27")},
		{"a municipalità with no quartiere in the stradario", Query{Street: "Largo Roberto Pane"}, resolved(1, "", BasisStreet, "24/73")},
		{"fuorigrotta", Query{Street: "Via Giulio Cesare", Civico: "10"}, want{Resolved, 10, "Fuorigrotta", BasisStreet, "25"}},

		// A street across boundaries needs the civico or the quartiere.
		{"toledo alone", Query{Street: "Via Toledo"}, want{status: Ambiguous}},
		{"toledo, san ferdinando part", Query{Street: "Via Toledo", Civico: "258"}, resolved(1, "San Ferdinando", BasisCivico, "24/73")},
		{"toledo, montecalvario part", Query{Street: "Via Toledo", Civico: "324"}, resolved(2, "Montecalvario", BasisCivico, "31")},
		{"toledo, san giuseppe part", Query{Street: "Via Toledo", Civico: "133"}, resolved(2, "San Giuseppe", BasisCivico, "31")},
		{"civico with a letter", Query{Street: "Via Toledo", Civico: "258/B"}, resolved(1, "San Ferdinando", BasisCivico, "24/73")},
		{"civico beyond every range", Query{Street: "Via Toledo", Civico: "999"}, want{status: Ambiguous}},
		{"no civico number", Query{Street: "Via Toledo", Civico: "snc"}, want{status: Ambiguous}},
		{"toledo by quartiere", Query{Street: "Via Toledo", Quartiere: "Montecalvario"}, resolved(2, "Montecalvario", BasisQuartiere, "31")},
		{"quartiere settles it where the civico is outside the ranges", Query{Street: "Via Toledo", Civico: "999", Quartiere: "S. Ferdinando"}, resolved(1, "San Ferdinando", BasisQuartiere, "24/73")},
		{"quartiere and civico agree", Query{Street: "Via Toledo", Civico: "258", Quartiere: "San Ferdinando"}, resolved(1, "San Ferdinando", BasisQuartiere, "24/73")},
		{"quartiere and civico disagree", Query{Street: "Via Toledo", Civico: "258", Quartiere: "Montecalvario"}, want{status: Ambiguous}},
		{"quartiere the street does not touch", Query{Street: "Via Toledo", Quartiere: "Vomero"}, want{status: Ambiguous}},
		{"not a quartiere", Query{Street: "Via Toledo", Quartiere: "Centro"}, want{status: Ambiguous}},

		// Parity in the ranges: Via Tasso has odd and even runs.
		{"tasso odd, chiaia", Query{Street: "Via Torquato Tasso", Civico: "127"}, resolved(1, "Chiaia", BasisCivico, "24/73")},
		{"tasso odd, vomero", Query{Street: "Via Torquato Tasso", Civico: "283"}, resolved(5, "Vomero", BasisCivico, "27")},
		{"tasso even, chiaia", Query{Street: "Via Torquato Tasso", Civico: "284"}, resolved(1, "Chiaia", BasisCivico, "24/73")},
		{"foria odd, san carlo", Query{Street: "Via Foria", Civico: "73"}, resolved(3, "San Carlo all'Arena", BasisCivico, "29")},
		{"foria even, san lorenzo", Query{Street: "Via Foria", Civico: "76"}, resolved(4, "San Lorenzo", BasisCivico, "33")},
		{"foria, civico in the odd run of stella", Query{Street: "Via Foria", Civico: "13"}, resolved(3, "Stella", BasisCivico, "29")},
		{"single civico range", Query{Street: "Piazzetta Sedil Capuano", Civico: "239"}, resolved(4, "San Lorenzo", BasisCivico, "33")},
		{"single civico range, other side", Query{Street: "Piazzetta Sedil Capuano", Civico: "241"}, resolved(2, "Pendino", BasisCivico, "31")},

		// A letter after the upper bound: 2003 says Avvocata goes up to 52/A.
		{"civico 52", Query{Street: "Via Pessina Enrico", Civico: "52"}, resolved(2, "Avvocata", BasisCivico, "31")},
		{"civico 52/A", Query{Street: "Via Pessina Enrico", Civico: "52/A"}, resolved(2, "Avvocata", BasisCivico, "31")},
		{"civico 52/B is in no range", Query{Street: "Via Pessina Enrico", Civico: "52/B"}, want{status: Ambiguous}},
		{"civico 54 is Stella's alone", Query{Street: "Via Pessina Enrico", Civico: "54"}, resolved(3, "Stella", BasisCivico, "29")},

		// A quartiere with no range leaves a part of the street unknown: a civico
		// in the others' ranges settles nothing.
		{"de roberto, a quartiere without ranges", Query{Street: "Via De Roberto Domenico", Civico: "31"}, want{status: Ambiguous}},
		{"piazza leonardo, a quartiere without ranges", Query{Street: "Piazza Leonardo", Civico: "5"}, want{status: Ambiguous}},
		// Clauses written without pari/dispari that overlap are an odd and an even run.
		{"vittorio veneto, unmarked overlapping clauses", Query{Street: "Via Vittorio Veneto", Civico: "401"}, want{status: Ambiguous}},

		// A named quartiere the street never touches doubts a partial match only.
		{"partial street, quartiere it is in", Query{Street: "Via Cilea", Quartiere: "Vomero"}, resolved(5, "Vomero", BasisStreet, "27")},
		{"partial street, quartiere it is not in", Query{Street: "Via Cilea", Quartiere: "Chiaia"}, want{status: Ambiguous}},
		{"exact street, quartiere it is not in", Query{Street: "Via Cilea Francesco", Quartiere: "Chiaia"}, resolved(5, "Vomero", BasisStreet, "27")},
		{"an unknown quartiere text on a street in one municipalità", Query{Street: "Via Alabardieri", Quartiere: "Mergellina"}, resolved(1, "Chiaia", BasisStreet, "24/73")},

		// Not enough, or nothing.
		{"nothing like it", Query{Street: "Via Inesistente Qwerty"}, want{status: NotFound}},
		{"no street", Query{Street: "   "}, want{status: NotFound}},
		{"a partial name that fits streets in two municipalità", Query{Street: "Via Roma"}, want{status: Ambiguous}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := Lookup(c.q)
			if r.Status != c.want.status {
				t.Fatalf("status %s, want %s: %+v", r.Status, c.want.status, r)
			}
			if r.Status != Resolved {
				if r.Municipalita != 0 || r.Distretto != "" {
					t.Errorf("an unresolved lookup must not name a municipalità: %+v", r)
				}
				if r.Reason == "" {
					t.Errorf("an unresolved lookup says why: %+v", r)
				}
				return
			}
			if r.Municipalita != c.want.mun || r.Quartiere != c.want.quartiere || r.Basis != c.want.basis || r.Distretto != c.want.distretto {
				t.Errorf("got mun %d quartiere %q basis %s distretto %q; want %+v", r.Municipalita, r.Quartiere, r.Basis, r.Distretto, c.want)
			}
		})
	}
}

func TestAmbiguousOffersTheQuartieri(t *testing.T) {
	r := Lookup(Query{Street: "Via Toledo"})
	if r.Status != Ambiguous {
		t.Fatal(r)
	}
	if got, want := r.QuartiereChoices(), []string{"San Ferdinando", "Montecalvario", "San Giuseppe"}; !slices.Equal(got, want) {
		t.Errorf("choices %v, want %v", got, want)
	}
	// Whatever the person answers resolves it.
	for _, q := range r.QuartiereChoices() {
		if got := Lookup(Query{Street: "Via Toledo", Quartiere: q}); got.Status != Resolved {
			t.Errorf("answering %q left %+v", q, got)
		}
	}
}

func TestEveryAmbiguousStreetIsSettledByItsQuartieri(t *testing.T) {
	ambiguous := 0
	for _, s := range load() {
		r := Lookup(Query{Street: s.name})
		if r.Status != Ambiguous {
			continue
		}
		ambiguous++
		for _, q := range r.QuartiereChoices() {
			if got := Lookup(Query{Street: s.name, Quartiere: q}); got.Status != Resolved {
				t.Errorf("%q in %s: %+v", s.name, q, got)
			}
		}
	}
	if ambiguous < 100 {
		t.Errorf("only %d ambiguous streets: the stradario has about 145", ambiguous)
	}
}

func TestPartialNameIsFlagged(t *testing.T) {
	r := Lookup(Query{Street: "Via Cilea"})
	if r.Status != Resolved || !r.Partial || r.Municipalita != 5 {
		t.Errorf("%+v", r)
	}
	if exact := Lookup(Query{Street: "Via Cilea Francesco"}); exact.Partial {
		t.Errorf("an exact name is not partial: %+v", exact)
	}
}

func TestNameTokens(t *testing.T) {
	for _, c := range []struct{ a, b string }{
		{"Via Torquato Tasso", "VIA TASSO TORQUATO"},
		{"Via dei Tribunali", "via Tribunali"},
		{"Via S. Giacomo dei Capri", "VIA S.GIACOMO DEI CAPRI"},
		{"Via Santa Brigida", "VIA S.BRIGIDA"},
		{"Via Trinità degli Spagnoli", "VIA TRINITA' DEGLI SPAGNOLI"},
		{"P.zza Dante", "PIAZZA DANTE"},
		{"V.le Colli Aminei", "VIALE COLLI AMINEI"},
		{"Vicolo Belledonne a Chiaia", "VICO BELLEDONNE A CHIAIA"},
	} {
		if !slices.Equal(nameTokens(c.a), nameTokens(c.b)) {
			t.Errorf("%q = %v, %q = %v", c.a, nameTokens(c.a), c.b, nameTokens(c.b))
		}
	}
	if slices.Equal(nameTokens("Via Chiaia"), nameTokens("Vico Chiaia")) {
		t.Error("the street type matters")
	}
}

func TestCivicoNumber(t *testing.T) {
	for in, want := range map[string]struct {
		n      int
		letter string
	}{
		"12": {12, ""}, " 12 ": {12, ""}, "12/B": {12, "B"}, "12B": {12, "B"}, "12 bis": {12, "BIS"}, "12 / b": {12, "B"},
		"7": {7, ""}, "432": {432, ""}, "52/a": {52, "A"},
	} {
		if n, letter, ok := civicoNumber(in); !ok || n != want.n || letter != want.letter {
			t.Errorf("civicoNumber(%q) = %d, %q, %v", in, n, letter, ok)
		}
	}
	for _, in := range []string{"", "snc", "bis", "0", "A12", "123456"} {
		if n, _, ok := civicoNumber(in); ok {
			t.Errorf("civicoNumber(%q) = %d, want none", in, n)
		}
	}
}

func TestCivicoSpan(t *testing.T) {
	odd, even, any := civicoSpan{lo: 1, hi: 9, parity: 1}, civicoSpan{lo: 2, hi: 10, parity: 2}, civicoSpan{lo: 3, hi: 5}
	for _, c := range []struct {
		sp   civicoSpan
		n    int
		want bool
	}{
		{odd, 1, true}, {odd, 2, false}, {odd, 9, true}, {odd, 11, false},
		{even, 2, true}, {even, 3, false}, {even, 10, true}, {even, 12, false}, {even, 0, false},
		{any, 3, true}, {any, 4, true}, {any, 5, true}, {any, 6, false}, {any, 2, false},
	} {
		if got := c.sp.has(c.n, ""); got != c.want {
			t.Errorf("%+v has %d = %v", c.sp, c.n, got)
		}
	}
	// A range that ends at 52/A takes 52 and 52/A but not 52/B.
	upTo52A := civicoSpan{lo: 1, hi: 52, hiLetter: "A"}
	for letter, want := range map[string]bool{"": true, "A": true, "B": false, "BIS": false, "AB": false} {
		if got := upTo52A.has(52, letter); got != want {
			t.Errorf("1-52/A has 52%s = %v", letter, got)
		}
	}
	if !upTo52A.has(51, "Z") {
		t.Error("51/Z is inside 1-52/A")
	}
}

// Every street that has civico ranges has them for each of its quartieri.
func TestRangesCoverEveryQuartiereOfTheirStreet(t *testing.T) {
	n := 0
	for i := range load() {
		s := &load()[i]
		if s.ranges == nil {
			continue
		}
		n++
		if !s.complete() {
			t.Errorf("%q has ranges for %d quartieri, but crosses %d", s.name, len(s.ranges), len(s.segments))
		}
		for q, spans := range s.ranges {
			for _, sp := range spans {
				if sp.lo > sp.hi || sp.lo < 1 || sp.parity < 0 || sp.parity > 2 {
					t.Errorf("%q %s: bad span %+v", s.name, q, sp)
				}
			}
		}
	}
	if n < 100 {
		t.Errorf("only %d streets have ranges", n)
	}
}

// Wrapped cells of the PDF once doubled or lost words of a name.
func TestWrappedNamesAreWhole(t *testing.T) {
	names := map[string]bool{}
	for _, s := range load() {
		names[s.name] = true
		words := strings.Fields(s.name)
		for i := 1; i < len(words); i++ {
			// PARCO PARCO VIRGILIANO is as the PDFs write it (type PARCO, name PARCO VIRGILIANO).
			if words[i] == words[i-1] && s.name != "PARCO PARCO VIRGILIANO" && !strings.ContainsAny(words[i], "0123456789") {
				t.Errorf("%q repeats %q", s.name, words[i])
			}
		}
	}
	for _, want := range []string{
		"TRAVERSA 1^ VILLA BISIGNANO", "PROLUNGAMENTO TRAVERSA 4^ VILLA BISIGNANO",
		"TRAVERSA COMUNALE DI VIA DEL COLLETTORE", "PIAZZA DELLA FRANCESCA PIERO",
	} {
		if !names[want] {
			t.Errorf("%q is not a street of the data", want)
		}
	}
	if r := Lookup(Query{Street: "Piazza della Francesca Piero"}); r.Status != Resolved || r.Partial || r.Municipalita != 9 {
		t.Errorf("%+v", r)
	}
}
