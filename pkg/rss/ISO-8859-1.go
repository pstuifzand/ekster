package rss

import (
	"bytes"
	"errors"
	"io"
	"unicode/utf8"
)

// ISO-8859-1 support

type charsetISO88591er struct {
	r   io.ByteReader
	buf *bytes.Buffer
}

func newCharsetISO88591(r io.Reader) *charsetISO88591er {
	buf := bytes.NewBuffer(make([]byte, 0, utf8.UTFMax))
	return &charsetISO88591er{r.(io.ByteReader), buf}
}

func (cs *charsetISO88591er) ReadByte() (b byte, err error) {
	// http://unicode.org/Public/MAPPINGS/ISO8859/8859-1.TXT
	// Date: 1999 July 27; Last modified: 27-Feb-2001 05:08
	if cs.buf.Len() <= 0 {
		r, err := cs.r.ReadByte()
		if err != nil {
			return 0, err
		}
		if r < utf8.RuneSelf {
			return r, nil
		}
		cs.buf.WriteRune(rune(r))
	}
	return cs.buf.ReadByte()
}

func (cs *charsetISO88591er) Read(p []byte) (int, error) {
	// Use ReadByte method.
	return 0, errors.New("use ReadByte()")
}

func isCharsetISO88591(charset string) bool {
	// http://www.iana.org/assignments/character-sets
	// (last updated 2010-11-04)
	names := []string{
		// Name
		"ISO_8859-1:1987",
		// Alias (preferred MIME name)
		"ISO-8859-1",
		// Aliases
		"iso-ir-100",
		"ISO_8859-1",
		"latin1",
		"l1",
		"IBM819",
		"CP819",
		"csISOLatin1",
	}
	return isCharset(charset, names)
}
