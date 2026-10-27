// Package napoli says which municipalità, and so which ASL distretto, an
// address in the Comune di Napoli is in. It works offline from two published
// tables, never from a geocoder, so an address is never sent anywhere.
//
// The stradario of the Comune (ordinato per municipalità, aggiornamento al
// 22/12/2025, https://www.comune.napoli.it/stradario) gives each street's
// quartieri, each in one municipalità. It has no house numbers, so about 4%
// of streets, among them Via Toledo, Via dei Tribunali and Via Foria, cross a
// municipalità boundary. For about half of those the 2003 COSAP stradario
// gives civico ranges per quartiere; they are old, so a civico that falls in
// none of a street's ranges, or in several, is left unresolved. A lookup
// that can't decide returns Ambiguous with the choices to ask about; it never
// guesses. internal/napoli/gen rebuilds data/stradario.json from the PDFs.
package napoli

import (
	_ "embed"
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed data/stradario.json
var stradarioJSON []byte

// Status is how far a lookup got.
type Status string

const (
	Resolved  Status = "resolved"  // one municipalità
	Ambiguous Status = "ambiguous" // the street crosses a boundary: ask, using Candidates
	NotFound  Status = "not_found" // no street of the stradario fits
)

// Basis is what settled a Resolved lookup.
type Basis string

const (
	BasisStreet    Basis = "street"    // every quartiere of the street is in one municipalità
	BasisCivico    Basis = "civico"    // the civico falls in one quartiere's range (2003 data)
	BasisQuartiere Basis = "quartiere" // the caller named the quartiere
)

// Query is an address as the vault holds it: the street without its civico
// (residenza.indirizzo), the civico apart (residenza.civico), and optionally a
// quartiere the person named.
type Query struct {
	Street    string
	Civico    string
	Quartiere string
}

// Candidate is one place a street lies in.
type Candidate struct {
	Street       string // as the stradario writes it
	Quartiere    string // "" when the stradario gives none
	Municipalita int
}

// Result is the outcome of Lookup.
type Result struct {
	Status       Status
	Municipalita int    // set when Resolved
	Distretto    string // ASL distretto of Municipalita, set when Resolved
	Quartiere    string // set when Resolved and the quartiere is known
	Basis        Basis
	// Partial means the street was matched on fewer words than the stradario's
	// name ("Via Tasso" for "VIA TASSO TORQUATO"); a caller should confirm it.
	Partial bool
	Streets []string
	// Candidates are the choices left when Ambiguous (or all of them when
	// Resolved by civico or quartiere), most useful for asking the quartiere.
	Candidates []Candidate
	Reason     string // why a lookup is not Resolved
}

// QuartiereChoices lists the distinct quartieri among the candidates, to ask
// "which quartiere is your address in?".
func (r Result) QuartiereChoices() []string {
	var out []string
	for _, c := range r.Candidates {
		if c.Quartiere == "" {
			continue
		}
		if q, ok := quartiereByLabel[c.Quartiere]; ok && !slices.Contains(out, q.Name) {
			out = append(out, q.Name)
		}
	}
	return out
}

// InNapoli reports whether a comune is Napoli; outside it a lookup means
// nothing (use the ASL of the comune instead).
func InNapoli(comune string) bool { return foldName(comune) == "NAPOLI" }

type civicoSpan struct {
	lo, hi, parity int    // parity: 0 any, 1 odd, 2 even
	hiLetter       string // the letter the upper bound ends with ("52/A"), if any
}

type segment struct {
	quartiere string // stradario label
	mun       int
}

type street struct {
	name     string
	tokens   []string
	segments []segment
	ranges   map[string][]civicoSpan // by quartiere label, only where known
}

// complete reports that every quartiere of the street has civico ranges. A
// street with a quartiere that has none has a part whose civici are unknown, so
// a civico in the other ranges proves nothing.
func (s *street) complete() bool {
	if s.ranges == nil {
		return false
	}
	for _, sg := range s.segments {
		if len(s.ranges[sg.quartiere]) == 0 {
			return false
		}
	}
	return true
}

var load = sync.OnceValue(func() []street {
	var doc struct {
		Streets []struct {
			Name     string              `json:"n"`
			Segments [][]any             `json:"s"`
			Ranges   [][]json.RawMessage `json:"r"`
		} `json:"streets"`
	}
	if err := json.Unmarshal(stradarioJSON, &doc); err != nil {
		panic("napoli: corrupt data/stradario.json: " + err.Error())
	}
	out := make([]street, 0, len(doc.Streets))
	for _, s := range doc.Streets {
		st := street{name: s.Name, tokens: nameTokens(s.Name)}
		for _, sg := range s.Segments {
			st.segments = append(st.segments, segment{quartiere: sg[0].(string), mun: int(sg[1].(float64))})
		}
		for _, r := range s.Ranges {
			var q string
			var spans [][]any // [lo, hi, parity] or [lo, hi, parity, "A"]
			if json.Unmarshal(r[0], &q) != nil || json.Unmarshal(r[1], &spans) != nil {
				panic("napoli: corrupt civico range in data/stradario.json: " + s.Name)
			}
			if st.ranges == nil {
				st.ranges = map[string][]civicoSpan{}
			}
			for _, sp := range spans {
				if len(sp) < 3 {
					panic("napoli: corrupt civico range in data/stradario.json: " + s.Name)
				}
				lo, _ := sp[0].(float64)
				hi, _ := sp[1].(float64)
				parity, _ := sp[2].(float64)
				span := civicoSpan{lo: int(lo), hi: int(hi), parity: int(parity)}
				if len(sp) > 3 {
					span.hiLetter, _ = sp[3].(string)
				}
				st.ranges[q] = append(st.ranges[q], span)
			}
		}
		out = append(out, st)
	}
	return out
})

// civicoNumber is the number a civico starts with and the letters after it,
// upper-case: 12/B is 12 and "B", 12 bis is 12 and "BIS".
func civicoNumber(s string) (n int, letter string, ok bool) {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 || end > 5 {
		return 0, "", false
	}
	n, err := strconv.Atoi(s[:end])
	if err != nil || n <= 0 {
		return 0, "", false
	}
	letter = strings.ToUpper(strings.Map(func(r rune) rune {
		if r == '/' || r == '-' || r == ' ' || r == '.' {
			return -1
		}
		return r
	}, s[end:]))
	return n, letter, true
}

// has reports whether the civico (its number and letters) is in the span. A
// range that ends at 52/A does not take 52/B.
func (sp civicoSpan) has(n int, letter string) bool {
	if n < sp.lo || n > sp.hi {
		return false
	}
	if n == sp.hi && sp.hiLetter != "" && letter > sp.hiLetter {
		return false
	}
	return sp.parity == 0 || sp.parity == 1 && n%2 == 1 || sp.parity == 2 && n%2 == 0
}

// Lookup finds the municipalità of an address in the Comune di Napoli.
func Lookup(q Query) Result {
	want := nameTokens(q.Street)
	if len(want) == 0 {
		return Result{Status: NotFound, Reason: "no street given"}
	}
	streets := load()
	var hit []*street
	partial := false
	for i := range streets {
		if slices.Equal(streets[i].tokens, want) {
			hit = append(hit, &streets[i])
		}
	}
	if len(hit) == 0 {
		partial = true
		for i := range streets {
			if containsAll(streets[i].tokens, want) {
				hit = append(hit, &streets[i])
			}
		}
	}
	if len(hit) == 0 {
		return Result{Status: NotFound, Reason: "no street of the stradario fits \"" + strings.TrimSpace(q.Street) + "\""}
	}

	res := Result{Partial: partial}
	for _, s := range hit {
		res.Streets = append(res.Streets, s.name)
	}
	type cand struct {
		s   *street
		seg segment
	}
	var cands []cand
	for _, s := range hit {
		for _, sg := range s.segments {
			cands = append(cands, cand{s, sg})
		}
	}
	toCandidates := func(cs []cand) []Candidate {
		out := make([]Candidate, 0, len(cs))
		for _, c := range cs {
			out = append(out, Candidate{Street: c.s.name, Quartiere: c.seg.quartiere, Municipalita: c.seg.mun})
		}
		sort.SliceStable(out, func(i, j int) bool {
			a, b := out[i], out[j]
			if a.Municipalita != b.Municipalita {
				return a.Municipalita < b.Municipalita
			}
			if a.Quartiere != b.Quartiere {
				return a.Quartiere < b.Quartiere
			}
			return a.Street < b.Street
		})
		return out
	}
	muns := func(cs []cand) []int {
		var m []int
		for _, c := range cs {
			if !slices.Contains(m, c.seg.mun) {
				m = append(m, c.seg.mun)
			}
		}
		return m
	}
	resolve := func(cs []cand, basis Basis) Result {
		res.Status, res.Basis = Resolved, basis
		res.Municipalita = cs[0].seg.mun
		res.Distretto = distretti[res.Municipalita]
		res.Candidates = toCandidates(cs)
		if qs := quartiereSet(cs, func(c cand) string { return c.seg.quartiere }); len(qs) == 1 && qs[0] != "" {
			res.Quartiere = quartiereByLabel[qs[0]].Name
		}
		return res
	}
	// A quartiere named that the street never touches says the street found may
	// not be the person's; only a partial match is in doubt.
	if partial && strings.TrimSpace(q.Quartiere) != "" {
		if qq, ok := FindQuartiere(q.Quartiere); ok && !slices.ContainsFunc(cands, func(c cand) bool { return c.seg.quartiere == qq.label }) {
			res.Status, res.Candidates = Ambiguous, toCandidates(cands)
			res.Reason = "the street has no part in " + qq.Name
			return res
		}
	}
	if len(muns(cands)) == 1 {
		return resolve(cands, BasisStreet)
	}

	// The street crosses a boundary: narrow it by what the person told us.
	narrowed, basis := cands, Basis("")
	var byQuartiere []cand
	if strings.TrimSpace(q.Quartiere) != "" {
		qq, ok := FindQuartiere(q.Quartiere)
		if !ok {
			res.Status, res.Candidates = Ambiguous, toCandidates(cands)
			res.Reason = "\"" + strings.TrimSpace(q.Quartiere) + "\" is not a quartiere of Napoli"
			return res
		}
		for _, c := range cands {
			if c.seg.quartiere == qq.label {
				byQuartiere = append(byQuartiere, c)
			}
		}
		if len(byQuartiere) == 0 {
			res.Status, res.Candidates = Ambiguous, toCandidates(cands)
			res.Reason = "the street has no part in " + qq.Name
			return res
		}
		narrowed, basis = byQuartiere, BasisQuartiere
	}
	// Ranges belong to one street; a civico in no range, or in the ranges of
	// several quartieri, says nothing.
	if n, letter, ok := civicoNumber(q.Civico); ok && len(hit) == 1 && hit[0].complete() {
		var byCivico []cand
		for _, c := range cands {
			for _, sp := range hit[0].ranges[c.seg.quartiere] {
				if sp.has(n, letter) {
					byCivico = append(byCivico, c)
					break
				}
			}
		}
		if len(byCivico) == 1 {
			switch {
			case basis == "":
				narrowed, basis = byCivico, BasisCivico
			case narrowed[0].seg.quartiere != byCivico[0].seg.quartiere:
				res.Status, res.Candidates = Ambiguous, toCandidates(cands)
				res.Reason = "the quartiere named and the civico (by the 2003 ranges) point to different places"
				return res
			}
		}
	}
	if len(muns(narrowed)) == 1 {
		return resolve(narrowed, basis)
	}
	res.Status, res.Candidates = Ambiguous, toCandidates(narrowed)
	res.Reason = "the street crosses municipalità boundaries; the quartiere decides"
	return res
}

func quartiereSet[T any](xs []T, key func(T) string) []string {
	var out []string
	for _, x := range xs {
		if k := key(x); !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}
