package crypto

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

func encryptStream(t *testing.T, key, aad, plaintext []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := NewStreamWriter(&buf, key, aad)
	if err != nil {
		t.Fatal(err)
	}
	// Odd-sized writes exercise chunk boundaries.
	for p := plaintext; len(p) > 0; {
		n := min(len(p), 7000)
		if _, err := w.Write(p[:n]); err != nil {
			t.Fatal(err)
		}
		p = p[n:]
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decryptStream(key, aad, ciphertext []byte) ([]byte, error) {
	r, err := NewStreamReader(bytes.NewReader(ciphertext), key, aad)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func TestStreamRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	aad := []byte("header")
	for _, size := range []int{0, 1, streamChunkSize - 1, streamChunkSize, streamChunkSize + 1, 3 * streamChunkSize, 3*streamChunkSize + 12345} {
		plaintext := make([]byte, size)
		rand.Read(plaintext)
		ct := encryptStream(t, key, aad, plaintext)
		got, err := decryptStream(key, aad, ct)
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		if !bytes.Equal(got, plaintext) {
			t.Fatalf("size %d: plaintext mismatch", size)
		}
	}
}

func TestStreamRejectsTampering(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	aad := []byte("header")
	plaintext := make([]byte, 2*streamChunkSize+100)
	rand.Read(plaintext)
	ct := encryptStream(t, key, aad, plaintext)
	full := streamChunkSize + 16

	otherKey := make([]byte, 32)
	rand.Read(otherKey)

	cases := map[string]struct {
		key, aad, ct []byte
	}{
		"wrong key":        {otherKey, aad, ct},
		"wrong aad":        {key, []byte("other"), ct},
		"flipped bit":      {key, aad, append(append([]byte{}, ct[:10]...), append([]byte{ct[10] ^ 1}, ct[11:]...)...)},
		"cut at boundary":  {key, aad, ct[:2*full]},
		"cut mid-chunk":    {key, aad, ct[:len(ct)-5]},
		"final chunk gone": {key, aad, ct[:full]},
		"data appended":    {key, aad, append(append([]byte{}, ct...), make([]byte, full)...)},
		"chunks swapped":   {key, aad, append(append(append([]byte{}, ct[full:2*full]...), ct[:full]...), ct[2*full:]...)},
		"empty":            {key, aad, nil},
	}
	for name, c := range cases {
		if _, err := decryptStream(c.key, c.aad, c.ct); !errors.Is(err, ErrStreamCorrupt) {
			t.Errorf("%s: expected ErrStreamCorrupt, got %v", name, err)
		}
	}
}
