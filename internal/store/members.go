package store

import "sort"

// Household members' fields and documents are stored in categories
// prefixed with the member's slug and a colon (leo:cie). ';' sorts right
// after ':', so [slug:, slug;) is exactly one member's categories, without
// LIKE (where '_' would be a wildcard).

// MemberExists reports whether a member has any field or document.
func (d *DB) MemberExists(slug string) (bool, error) {
	lo, hi := slug+":", slug+";"
	var n int
	err := d.conn.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM vault_fields WHERE category >= ? AND category < ?)
		     OR EXISTS (SELECT 1 FROM vault_documents WHERE category >= ? AND category < ?)`,
		lo, hi, lo, hi,
	).Scan(&n)
	return n == 1, err
}

// MemberSlugs returns the slugs of every member with a field or document,
// sorted.
func (d *DB) MemberSlugs() ([]string, error) {
	rows, err := d.conn.Query(
		`SELECT DISTINCT substr(category, 1, instr(category, ':') - 1) FROM vault_fields WHERE instr(category, ':') > 0
		 UNION
		 SELECT DISTINCT substr(category, 1, instr(category, ':') - 1) FROM vault_documents WHERE instr(category, ':') > 0`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var slugs []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	return slugs, rows.Err()
}
