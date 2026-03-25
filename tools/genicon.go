//go:build ignore

// genicon generates a VRShare app icon: wifi signal inside a rounded speech bubble.
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"strconv"
)

func main() {
	size := 512
	if len(os.Args) > 2 {
		size, _ = strconv.Atoi(os.Args[2])
	}
	S := float64(size)
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	white := color.RGBA{255, 255, 255, 255}
	black := color.RGBA{30, 30, 30, 255}

	// All values as fractions of S
	outline := S * 0.035
	bubbleL := S * 0.06
	bubbleT := S * 0.06
	bubbleR := S * 0.94
	bubbleB := S * 0.74
	radius := S * 0.10

	// Tail points
	tailX1 := S * 0.15
	tailY1 := bubbleB - 1
	tailX2 := S * 0.35
	tailY2 := bubbleB - 1
	tailX3 := S * 0.15
	tailY3 := S * 0.92

	// Draw black outline bubble
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			inBubbleOuter := inRoundedRect(fx, fy, bubbleL-outline, bubbleT-outline, bubbleR+outline, bubbleB+outline, radius+outline)
			inTailOuter := pointInTriangle(fx, fy, tailX1-outline*0.7, tailY1, tailX2+outline*0.5, tailY2, tailX3-outline*0.7, tailY3+outline)
			if inBubbleOuter || inTailOuter {
				img.SetRGBA(x, y, black)
			}
		}
	}

	// Draw white fill
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			inBubble := inRoundedRect(fx, fy, bubbleL, bubbleT, bubbleR, bubbleB, radius)
			inTail := pointInTriangle(fx, fy, tailX1, tailY1, tailX2, tailY2, tailX3, tailY3)
			if inBubble || inTail {
				img.SetRGBA(x, y, white)
			}
		}
	}

	// Wifi symbol - visually centered in bubble
	// The wifi symbol spans from the dot to the top of the outer arc.
	// To visually center it, place the midpoint of that span at the bubble center.
	dotR := S * 0.043
	arcThick := S * 0.055
	arcRadii := []float64{S * 0.127, S * 0.225, S * 0.322}
	wifiHeight := arcRadii[2] + arcThick + dotR // total visual height
	bubbleCenterY := (bubbleT + bubbleB) / 2
	// Place dot so that the midpoint of the wifi symbol is at bubble center
	wifiCx := (bubbleL + bubbleR) / 2
	wifiCy := bubbleCenterY + wifiHeight/2 - dotR

	for _, r := range arcRadii {
		drawArc(img, wifiCx, wifiCy, r, r+arcThick, -math.Pi*0.8, -math.Pi*0.2, black)
	}

	fillCircle(img, wifiCx, wifiCy, dotR, black)

	out, _ := os.Create(os.Args[1])
	defer out.Close()
	png.Encode(out, img)
}

func inRoundedRect(px, py, left, top, right, bottom, r float64) bool {
	if px < left || px > right || py < top || py > bottom {
		return false
	}
	corners := [][2]float64{
		{left + r, top + r},
		{right - r, top + r},
		{left + r, bottom - r},
		{right - r, bottom - r},
	}
	for i, c := range corners {
		inCornerX := (i%2 == 0 && px < left+r) || (i%2 == 1 && px > right-r)
		inCornerY := (i < 2 && py < top+r) || (i >= 2 && py > bottom-r)
		if inCornerX && inCornerY {
			dx, dy := px-c[0], py-c[1]
			return dx*dx+dy*dy <= r*r
		}
	}
	return true
}

func fillCircle(img *image.RGBA, cx, cy, r float64, c color.RGBA) {
	for py := int(cy - r - 1); py <= int(cy+r+1); py++ {
		for px := int(cx - r - 1); px <= int(cx+r+1); px++ {
			dx, dy := float64(px)-cx+0.5, float64(py)-cy+0.5
			if dx*dx+dy*dy <= r*r {
				img.SetRGBA(px, py, c)
			}
		}
	}
}

func drawArc(img *image.RGBA, cx, cy, rInner, rOuter, startAngle, endAngle float64, c color.RGBA) {
	for py := int(cy - rOuter - 1); py <= int(cy+rOuter+1); py++ {
		for px := int(cx - rOuter - 1); px <= int(cx+rOuter+1); px++ {
			dx, dy := float64(px)-cx+0.5, float64(py)-cy+0.5
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist < rInner || dist > rOuter {
				continue
			}
			angle := math.Atan2(dy, dx)
			if angle >= startAngle && angle <= endAngle {
				img.SetRGBA(px, py, c)
			}
		}
	}
}

func pointInTriangle(px, py, x1, y1, x2, y2, x3, y3 float64) bool {
	d1 := (px-x2)*(y1-y2) - (x1-x2)*(py-y2)
	d2 := (px-x3)*(y2-y3) - (x2-x3)*(py-y3)
	d3 := (px-x1)*(y3-y1) - (x3-x1)*(py-y1)
	hasNeg := (d1 < 0) || (d2 < 0) || (d3 < 0)
	hasPos := (d1 > 0) || (d2 > 0) || (d3 > 0)
	return !(hasNeg && hasPos)
}
