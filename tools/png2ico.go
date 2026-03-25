//go:build ignore

// png2ico converts a PNG file to a Windows ICO file with multiple sizes.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"

	"golang.org/x/image/draw"
)

func main() {
	if len(os.Args) != 3 {
		os.Stderr.WriteString("usage: png2ico input.png output.ico\n")
		os.Exit(1)
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()

	src, err := png.Decode(f)
	if err != nil {
		panic(err)
	}

	sizes := []int{16, 32, 48, 64, 128, 256}
	var entries []icoEntry
	var images [][]byte

	for _, s := range sizes {
		// Scale image
		dst := image.NewRGBA(image.Rect(0, 0, s, s))
		draw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

		// Encode as PNG (ICO supports embedded PNG for sizes >= 256, and it works for all sizes in modern Windows)
		var buf bytes.Buffer
		png.Encode(&buf, dst)
		data := buf.Bytes()

		w, h := uint8(s), uint8(s)
		if s >= 256 {
			w, h = 0, 0 // 0 means 256 in ICO format
		}

		entries = append(entries, icoEntry{
			Width:   w,
			Height:  h,
			Planes:  1,
			BitCnt:  32,
			Size:    uint32(len(data)),
		})
		images = append(images, data)
	}

	// Calculate offsets
	headerSize := 6
	entrySize := 16
	offset := uint32(headerSize + entrySize*len(entries))
	for i := range entries {
		entries[i].Offset = offset
		offset += entries[i].Size
	}

	// Write ICO
	out, err := os.Create(os.Args[2])
	if err != nil {
		panic(err)
	}
	defer out.Close()

	// Header: reserved(2) + type(2) + count(2)
	binary.Write(out, binary.LittleEndian, uint16(0))              // reserved
	binary.Write(out, binary.LittleEndian, uint16(1))              // type: icon
	binary.Write(out, binary.LittleEndian, uint16(len(entries)))   // count

	// Directory entries
	for _, e := range entries {
		binary.Write(out, binary.LittleEndian, e.Width)
		binary.Write(out, binary.LittleEndian, e.Height)
		binary.Write(out, binary.LittleEndian, uint8(0))  // color count
		binary.Write(out, binary.LittleEndian, uint8(0))  // reserved
		binary.Write(out, binary.LittleEndian, e.Planes)
		binary.Write(out, binary.LittleEndian, e.BitCnt)
		binary.Write(out, binary.LittleEndian, e.Size)
		binary.Write(out, binary.LittleEndian, e.Offset)
	}

	// Image data
	for _, data := range images {
		out.Write(data)
	}
}

type icoEntry struct {
	Width  uint8
	Height uint8
	Planes uint16
	BitCnt uint16
	Size   uint32
	Offset uint32
}
