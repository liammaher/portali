package wincred

import "testing"

// Secrets go in as Credential Manager stores passwords (UTF-16LE) and come
// back unchanged.
func TestEncoding(t *testing.T) {
	for _, s := range []string{"", "abc123", "pàssword€", "emoji 😀", "aa\x00bb"} {
		if got := decode(encode(s)); got != s {
			t.Errorf("%q came back as %q", s, got)
		}
	}
	if b := encode("ab"); len(b) != 4 || b[0] != 'a' || b[1] != 0 || b[2] != 'b' || b[3] != 0 {
		t.Errorf("encode(ab) = %v, want UTF-16LE", b)
	}
}
