// Package textenc reads text in whatever encoding it was saved in: UTF-8
// (with or without a BOM), UTF-16, or a legacy one — GBK/GB18030 and Big5
// for Chinese, Shift-JIS for Japanese. Subtitle files (Jusplay) and old
// text files in a notebook (Jusnote) come in all of them.
package textenc

import (
	"bytes"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// Decode reads b as text. The encodings are tried strictly in turn; a
// Shift-JIS reading that is almost all kanji is really Chinese (Japanese
// text always carries kana), and GB18030 accepts nearly any bytes, so it
// is last before Big5.
func Decode(b []byte) string {
	s, _ := DecodeName(b)
	return s
}

// DecodeName is Decode, and the name of the encoding it read.
func DecodeName(b []byte) (string, string) {
	switch {
	case bytes.HasPrefix(b, []byte{0xef, 0xbb, 0xbf}):
		return strings.ToValidUTF8(string(b[3:]), "�"), "utf-8"
	case bytes.HasPrefix(b, []byte{0xff, 0xfe}):
		return utf16Text(b[2:], false), "utf-16le"
	case bytes.HasPrefix(b, []byte{0xfe, 0xff}):
		return utf16Text(b[2:], true), "utf-16be"
	}
	if utf8.Valid(b) {
		return string(b), "utf-8"
	}
	if s, ok := strict(japanese.ShiftJIS, b); ok {
		k, c := count(s, isKana), count(s, isCJK)
		if c == 0 || float64(k)/float64(c) > 0.1 {
			return s, "shift_jis"
		}
	}
	if s, ok := strict(simplifiedchinese.GB18030, b); ok {
		return s, "gb18030"
	}
	if s, ok := strict(traditionalchinese.Big5, b); ok {
		return s, "big5"
	}
	s, _ := simplifiedchinese.GB18030.NewDecoder().Bytes(b)
	return string(s), "gb18030"
}

// strict decodes b with enc, failing on any sequence enc has no character
// for (the decoders put U+FFFD there; the input had none, being invalid
// UTF-8 only by being another encoding).
func strict(enc encoding.Encoding, b []byte) (string, bool) {
	out, err := enc.NewDecoder().Bytes(b)
	if err != nil || bytes.ContainsRune(out, utf8.RuneError) {
		return "", false
	}
	return string(out), true
}

func utf16Text(b []byte, bigEndian bool) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		if bigEndian {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			u[i] = uint16(b[2*i+1])<<8 | uint16(b[2*i])
		}
	}
	return string(utf16.Decode(u))
}

func isKana(r rune) bool { return r >= 0x3040 && r <= 0x30ff }
func isCJK(r rune) bool  { return isKana(r) || (r >= 0x4e00 && r <= 0x9fff) }

func count(s string, f func(rune) bool) int {
	n := 0
	for _, r := range s {
		if f(r) {
			n++
		}
	}
	return n
}
