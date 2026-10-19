package badge

import (
	"image"
	"image/color"
	"testing"
)

func TestBadgeSpec(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 800, 1200))
	for y := 0; y < 1200; y++ {
		for x := 0; x < 800; x++ {
			src.Set(x, y, color.RGBA{R: 0x33, G: 0x33, B: 0x33, A: 0xFF})
		}
	}

	cases := []struct {
		name string
		spec string
	}{
		{"legacy top-left", "zimu.png"},
		{"top-right only", "tr:uncensored.png"},
		{"both corners", "zimu.png,tr:uncensored.png"},
		{"spaces and upper case", " TL:zimu.png , TR:uncensored.png "},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := Badge(src, c.spec)
			if err != nil {
				t.Fatalf("badge %q: %v", c.spec, err)
			}
			if out.Bounds() != src.Bounds() {
				t.Fatalf("bounds changed: %v -> %v", src.Bounds(), out.Bounds())
			}
		})
	}
}

func TestBadgeUnknownName(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 200, 300))
	if _, err := Badge(src, "tr:no-such-badge.png"); err == nil {
		t.Fatal("expected error for unknown badge")
	}
}
