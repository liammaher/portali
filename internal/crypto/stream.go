package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// streamChunkSize is the plaintext size of every chunk but the last.
const streamChunkSize = 64 << 10

// ErrStreamCorrupt is returned when a stream fails authentication: wrong key,
// tampered or reordered chunks, or a truncated file.
var ErrStreamCorrupt = errors.New("encrypted stream is corrupt, truncated or was made with another key")

// Streams are AES-256-GCM in fixed-size chunks (the STREAM construction):
// each chunk's nonce is its index plus a flag marking the last chunk, and
// every chunk is bound to aad. Chunks therefore can't be reordered, dropped
// or appended, and a stream cut at a chunk boundary is detected. The final
// chunk is always shorter than streamChunkSize (possibly empty), which is how
// a reader recognizes it. A key must encrypt only one stream.

func streamAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

func streamNonce(index uint64, last bool) []byte {
	nonce := make([]byte, nonceLen)
	binary.BigEndian.PutUint64(nonce[3:11], index)
	if last {
		nonce[11] = 1
	}
	return nonce
}

type streamWriter struct {
	w      io.Writer
	aead   cipher.AEAD
	aad    []byte
	buf    []byte
	index  uint64
	closed bool
}

// NewStreamWriter returns a writer that encrypts to w. Close must be called
// to write the final chunk; it does not close w.
func NewStreamWriter(w io.Writer, key, aad []byte) (io.WriteCloser, error) {
	aead, err := streamAEAD(key)
	if err != nil {
		return nil, err
	}
	return &streamWriter{w: w, aead: aead, aad: aad, buf: make([]byte, 0, streamChunkSize)}, nil
}

func (s *streamWriter) Write(p []byte) (int, error) {
	if s.closed {
		return 0, errors.New("write to closed stream")
	}
	n := 0
	for len(p) > 0 {
		k := copy(s.buf[len(s.buf):streamChunkSize], p)
		s.buf = s.buf[:len(s.buf)+k]
		p = p[k:]
		n += k
		// A full chunk is only sealed once more data arrives, so a stream
		// whose length is a multiple of the chunk size still ends short.
		if len(s.buf) == streamChunkSize && len(p) > 0 {
			if err := s.seal(false); err != nil {
				return n, err
			}
		}
	}
	return n, nil
}

func (s *streamWriter) seal(last bool) error {
	if len(s.buf) == streamChunkSize && last {
		// Seal the full chunk, then an empty final one.
		if err := s.seal(false); err != nil {
			return err
		}
	}
	out := s.aead.Seal(nil, streamNonce(s.index, last), s.buf, s.aad)
	s.index++
	s.buf = s.buf[:0]
	_, err := s.w.Write(out)
	return err
}

func (s *streamWriter) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.seal(true)
}

type streamReader struct {
	r     io.Reader
	aead  cipher.AEAD
	aad   []byte
	chunk []byte
	plain []byte
	index uint64
	done  bool
}

// NewStreamReader returns a reader that decrypts a stream written by
// NewStreamWriter. Read returns ErrStreamCorrupt if any chunk fails, and
// io.EOF only after the authenticated final chunk.
func NewStreamReader(r io.Reader, key, aad []byte) (io.Reader, error) {
	aead, err := streamAEAD(key)
	if err != nil {
		return nil, err
	}
	return &streamReader{r: r, aead: aead, aad: aad, chunk: make([]byte, streamChunkSize+aead.Overhead())}, nil
}

func (s *streamReader) Read(p []byte) (int, error) {
	for len(s.plain) == 0 {
		if s.done {
			return 0, io.EOF
		}
		if err := s.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, s.plain)
	s.plain = s.plain[n:]
	return n, nil
}

func (s *streamReader) next() error {
	n, err := io.ReadFull(s.r, s.chunk)
	last := false
	switch {
	case err == io.ErrUnexpectedEOF:
		last = true
	case err == io.EOF:
		return ErrStreamCorrupt // ended without a final chunk
	case err != nil:
		return err
	}
	plain, err := s.aead.Open(s.chunk[:0], streamNonce(s.index, last), s.chunk[:n], s.aad)
	if err != nil {
		return ErrStreamCorrupt
	}
	s.index++
	s.plain = plain
	s.done = last
	return nil
}
