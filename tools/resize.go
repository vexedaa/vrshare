//go:build ignore

package main

import (
	"image"
	"image/png"
	"os"

	"golang.org/x/image/draw"
)

func main() {
	f, _ := os.Open(os.Args[1])
	defer f.Close()
	src, _ := png.Decode(f)

	// Find content bounds (crop transparent padding)
	bounds := src.Bounds()
	minX, minY := bounds.Max.X, bounds.Max.Y
	maxX, maxY := bounds.Min.X, bounds.Min.Y
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := src.At(x, y).RGBA()
			if a > 0 {
				if x < minX { minX = x }
				if y < minY { minY = y }
				if x > maxX { maxX = x }
				if y > maxY { maxY = y }
			}
		}
	}
	cropped := image.Rect(minX, minY, maxX+1, maxY+1)

	// Make it square by expanding the shorter side
	w, h := cropped.Dx(), cropped.Dy()
	if w > h {
		pad := (w - h) / 2
		cropped.Min.Y -= pad
		cropped.Max.Y = cropped.Min.Y + w
	} else if h > w {
		pad := (h - w) / 2
		cropped.Min.X -= pad
		cropped.Max.X = cropped.Min.X + h
	}

	// Scale to 256x256
	dst := image.NewRGBA(image.Rect(0, 0, 256, 256))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, cropped, draw.Over, nil)

	out, _ := os.Create(os.Args[2])
	defer out.Close()
	png.Encode(out, dst)
}
