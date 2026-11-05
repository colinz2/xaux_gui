package mytheme

import (
	"testing"

	"fyne.io/fyne/v2"
	"golang.org/x/image/font/sfnt"
)

func TestThemeUsesEmbeddedFont(t *testing.T) {
	for _, style := range []fyne.TextStyle{
		{},
		{Bold: true},
		{Italic: true},
		{Bold: true, Italic: true},
	} {
		resource := (MyTheme{}).Font(style)
		if resource != appFont {
			t.Fatalf("style %+v did not use embedded font", style)
		}
		parsed, err := sfnt.Parse(resource.Content())
		if err != nil {
			t.Fatalf("parse embedded font: %v", err)
		}
		index, err := parsed.GlyphIndex(nil, '会')
		if err != nil || index == 0 {
			t.Fatalf("embedded font lacks Chinese glyph: index=%d err=%v", index, err)
		}
	}
}

func TestThemeKeepsSpecialFonts(t *testing.T) {
	for _, style := range []fyne.TextStyle{{Symbol: true}, {Monospace: true}} {
		resource := (MyTheme{}).Font(style)
		if resource == nil || resource == appFont {
			t.Fatalf("style %+v did not retain its special font", style)
		}
	}
}
