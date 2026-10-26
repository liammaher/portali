package vault

import "strings"

// checkPersonRefs refuses a value naming a member who isn't in the vault
// (famiglia.genitori, famiglia.intestatario_scheda, pratiche persona).
func (v *Vault) checkPersonRefs(id, value string) error {
	_, local := SplitMember(id)
	parts := strings.SplitN(strings.ToLower(local), ".", 2)
	if len(parts) != 2 || !isPersonRefField(parts[0], parts[1]) {
		return nil
	}
	for _, ref := range PersonRefs(value) {
		if ref == OwnerRef {
			continue
		}
		ok, err := v.db.MemberExists(ref)
		if err != nil {
			return err
		}
		if !ok {
			return &ValidationError{Field: id, Msg: "there is no member \"" + ref + "\" yet: add them first with 'pvault member add " + ref + " --relazione ...'"}
		}
	}
	return nil
}
