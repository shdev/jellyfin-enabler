// Generiert die PWA-Icons für Jellyfin Enabler: dunkler Hintergrund mit dezentem
// Verlauf und Kippschalter in Akzentfarbe (--accent), abgerundete Enden.
// Vollflächig (kein Alpha), damit iOS-Home-Screen und maskable-Icons keine
// schwarzen Ecken bekommen. Glyphe liegt innerhalb der maskable-Safe-Zone
// (innere 80 %).
//
// Aufruf: go run ./cmd/genicons web/static/icons
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

var (
	bgTop    = color.NRGBA{0x17, 0x1b, 0x26, 0xff} // etwas heller als --surface
	bgBottom = color.NRGBA{0x0d, 0x0f, 0x14, 0xff} // --on-accent/dunkelster Ton
	accent   = color.NRGBA{0x6c, 0x8c, 0xff, 0xff} // --accent (dark)
)

// sdRoundRect: signierte Distanz zu einem abgerundeten Rechteck mit
// Zentrum (cx,cy), Halbgrößen (hx,hy) und Eckradius r. Negativ = innen.
func sdRoundRect(x, y, cx, cy, hx, hy, r float64) float64 {
	dx := math.Abs(x-cx) - (hx - r)
	dy := math.Abs(y-cy) - (hy - r)
	ax, ay := math.Max(dx, 0), math.Max(dy, 0)
	return math.Hypot(ax, ay) + math.Min(math.Max(dx, dy), 0) - r
}

// glyphDist: Kippschalter — eine abgerundete Kapsel, aus der der Knauf
// rechts ausgestanzt ist. Bleibt innerhalb der maskable-Safe-Zone (innere
// 80 %), damit iOS und Android nichts abschneiden.
func glyphDist(x, y float64) float64 {
	kapsel := sdRoundRect(x, y, 0.5, 0.5, 0.26, 0.145, 0.145)
	knauf := math.Hypot(x-0.605, y-0.5) - 0.088
	// max(a, -b) schneidet b aus a heraus.
	return math.Max(kapsel, -knauf)
}

// coverage: Akzent-Anteil eines Pixels (0..1) via 4x4-Supersampling.
func coverage(x, y, size int) float64 {
	const ss = 4
	hit := 0.0
	for i := 0; i < ss; i++ {
		for j := 0; j < ss; j++ {
			fx := (float64(x) + (float64(i)+0.5)/ss) / float64(size)
			fy := (float64(y) + (float64(j)+0.5)/ss) / float64(size)
			if glyphDist(fx, fy) <= 0 {
				hit++
			}
		}
	}
	return hit / (ss * ss)
}

func mix(a, b uint8, t float64) uint8 { return uint8(float64(a)*(1-t) + float64(b)*t + 0.5) }

func render(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		g := float64(y) / float64(size-1)
		base := color.NRGBA{
			mix(bgTop.R, bgBottom.R, g), mix(bgTop.G, bgBottom.G, g), mix(bgTop.B, bgBottom.B, g), 0xff,
		}
		for x := 0; x < size; x++ {
			t := coverage(x, y, size)
			img.SetNRGBA(x, y, color.NRGBA{
				mix(base.R, accent.R, t), mix(base.G, accent.G, t), mix(base.B, accent.B, t), 0xff,
			})
		}
	}
	return img
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: genicons <outdir>")
	}
	out := os.Args[1]
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}
	for name, size := range map[string]int{
		"apple-touch-icon.png": 180,
		"icon-192.png":         192,
		"icon-512.png":         512,
	} {
		f, err := os.Create(filepath.Join(out, name))
		if err != nil {
			log.Fatal(err)
		}
		if err := png.Encode(f, render(size)); err != nil {
			log.Fatal(err)
		}
		if err := f.Close(); err != nil {
			log.Fatal(err)
		}
		log.Printf("%s (%dx%d)", name, size, size)
	}
}
