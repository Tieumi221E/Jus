package textenc

import (
	"encoding/hex"
	"testing"
	"unicode/utf16"
)

func TestDecode(t *testing.T) {
	ja := "こんにちは、世界。今日は晴れです。"
	if got := Decode(append([]byte{0xef, 0xbb, 0xbf}, ja...)); got != ja {
		t.Errorf("UTF-8 BOM: %q", got)
	}
	le, be := []byte{0xff, 0xfe}, []byte{0xfe, 0xff}
	for _, c := range utf16.Encode([]rune(ja)) {
		le = append(le, byte(c), byte(c>>8))
		be = append(be, byte(c>>8), byte(c))
	}
	if Decode(le) != ja || Decode(be) != ja {
		t.Error("UTF-16")
	}
	for want, h := range map[string]string{
		"こんにちは、":  "82b182f182c982bf82cd8141",     // Shift-JIS
		"今天天气很好，": "bdf1ccecccecc6f8badcbac3a3ac", // GBK
		"今天天氣很好，": "a4b5a4d1a4d1aef0abdca66ea141", // Big5
	} {
		b, _ := hex.DecodeString(h)
		if got := Decode(b); got != want {
			t.Errorf("%s: %q", h, got)
		}
	}
	if _, name := DecodeName([]byte{0xbd, 0xf1, 0xcc, 0xec}); name != "gb18030" {
		t.Errorf("name %s", name)
	}
}
