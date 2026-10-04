// Command genicon renders the app icons and Discord assets.
//
//	go run ./tools/genicon
//
// Everything is drawn from signed distance functions with supersampling, so
// the output is crisp at every size and needs no source artwork.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

var (
	orange = color.NRGBA{0xE5, 0xA0, 0x0D, 0xFF}
	gray   = color.NRGBA{0x8A, 0x8F, 0x94, 0xFF}
	dark   = color.NRGBA{0x1F, 0x23, 0x26, 0xFF}
	white  = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
)

// shape returns the signed distance (negative inside) at a point in the unit
// square, plus the colour to paint there.
type layer struct {
	sdf func(x, y float64) float64
	col color.NRGBA
}

func render(size int, layers []layer) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 4 // supersamples per axis
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/ss) / float64(size)
					var cr, cg, cb, ca float64
					for _, l := range layers {
						if l.sdf(x, y) <= 0 {
							la := float64(l.col.A) / 255
							cr = cr*(1-la) + float64(l.col.R)*la
							cg = cg*(1-la) + float64(l.col.G)*la
							cb = cb*(1-la) + float64(l.col.B)*la
							ca = ca*(1-la) + la
						}
					}
					r, g, b, a = r+cr, g+cg, b+cb, a+ca
				}
			}
			n := float64(ss * ss)
			if a > 0 {
				img.SetNRGBA(px, py, color.NRGBA{
					uint8(math.Round(r / a)), uint8(math.Round(g / a)), uint8(math.Round(b / a)),
					uint8(math.Round(a / n * 255)),
				})
			}
		}
	}
	return img
}

func roundedSquare(inset, radius float64) func(x, y float64) float64 {
	return func(x, y float64) float64 {
		h := 0.5 - inset - radius
		dx := math.Max(math.Abs(x-0.5)-h, 0)
		dy := math.Max(math.Abs(y-0.5)-h, 0)
		return math.Hypot(dx, dy) - radius
	}
}

func circle(cx, cy, r float64) func(x, y float64) float64 {
	return func(x, y float64) float64 { return math.Hypot(x-cx, y-cy) - r }
}

func segment(ax, ay, bx, by, w float64) func(x, y float64) float64 {
	return func(x, y float64) float64 {
		px, py := x-ax, y-ay
		dx, dy := bx-ax, by-ay
		t := math.Max(0, math.Min(1, (px*dx+py*dy)/(dx*dx+dy*dy)))
		return math.Hypot(px-dx*t, py-dy*t) - w/2
	}
}

func union(fs ...func(x, y float64) float64) func(x, y float64) float64 {
	return func(x, y float64) float64 {
		d := math.Inf(1)
		for _, f := range fs {
			d = math.Min(d, f(x, y))
		}
		return d
	}
}

// chevron is the "play forward" mark used in the app icon.
func chevron(cx, cy, s, w float64) func(x, y float64) float64 {
	return union(
		segment(cx-s*0.45, cy-s, cx+s*0.55, cy, w),
		segment(cx+s*0.55, cy, cx-s*0.45, cy+s, w),
	)
}

// triangle is a rounded play triangle.
func triangle(cx, cy, s, round float64) func(x, y float64) float64 {
	ax, ay := cx-s*0.40, cy-s*0.55
	bx, by := cx-s*0.40, cy+s*0.55
	tx, ty := cx+s*0.55, cy
	return func(x, y float64) float64 {
		// distance to edges of the triangle, negative inside, then rounded
		edge := func(x1, y1, x2, y2 float64) float64 {
			nx, ny := y1-y2, x2-x1
			l := math.Hypot(nx, ny)
			return ((x-x1)*nx + (y-y1)*ny) / l
		}
		d := math.Max(edge(ax, ay, bx, by), math.Max(edge(bx, by, tx, ty), edge(tx, ty, ax, ay)))
		return d + round
	}
}

func appIcon(accent color.NRGBA) []layer {
	return []layer{
		{roundedSquare(0.02, 0.22), dark},
		{chevron(0.50, 0.50, 0.26, 0.15), accent},
	}
}

func badge(glyph func(x, y float64) float64) []layer {
	return []layer{
		{circle(0.5, 0.5, 0.48), orange},
		{glyph, white},
	}
}

func main() {
	out := "assets"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	must(os.MkdirAll(filepath.Join(out, "discord"), 0o755))

	sizes := []int{16, 20, 24, 32, 40, 48, 64, 256}
	writeICO(filepath.Join(out, "icon.ico"), sizes, appIcon(orange))
	writeICO(filepath.Join(out, "icon-idle.ico"), sizes, appIcon(gray))
	writePNG(filepath.Join(out, "icon.png"), render(512, appIcon(orange)))

	writePNG(filepath.Join(out, "discord", "plex.png"), render(1024, appIcon(orange)))
	writePNG(filepath.Join(out, "discord", "play.png"), render(512, badge(triangle(0.53, 0.5, 0.42, 0.03))))
	writePNG(filepath.Join(out, "discord", "pause.png"), render(512, badge(union(
		roundedBar(0.39, 0.5, 0.075, 0.22),
		roundedBar(0.61, 0.5, 0.075, 0.22),
	))))
}

func roundedBar(cx, cy, hw, hh float64) func(x, y float64) float64 {
	return segment(cx, cy-hh+hw, cx, cy+hh-hw, hw*2)
}

func writePNG(path string, img image.Image) {
	f, err := os.Create(path)
	must(err)
	defer f.Close()
	must(png.Encode(f, img))
}

// writeICO stores 256px as PNG and smaller sizes as 32-bit DIBs, which every
// Windows icon API understands.
func writeICO(path string, sizes []int, layers []layer) {
	var images [][]byte
	for _, s := range sizes {
		img := render(s, layers)
		if s >= 256 {
			var b bytes.Buffer
			must(png.Encode(&b, img))
			images = append(images, b.Bytes())
		} else {
			images = append(images, dib(img))
		}
	}
	var buf bytes.Buffer
	le := binary.LittleEndian
	_ = binary.Write(&buf, le, [3]uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		dim := uint8(s)
		if s >= 256 {
			dim = 0
		}
		_ = binary.Write(&buf, le, struct {
			W, H, Colors, Reserved uint8
			Planes, BPP            uint16
			Size, Offset           uint32
		}{dim, dim, 0, 0, 1, 32, uint32(len(images[i])), uint32(offset)})
		offset += len(images[i])
	}
	for _, b := range images {
		buf.Write(b)
	}
	must(os.WriteFile(path, buf.Bytes(), 0o644))
}

func dib(img *image.NRGBA) []byte {
	s := img.Bounds().Dx()
	var buf bytes.Buffer
	le := binary.LittleEndian
	maskStride := ((s + 31) / 32) * 4
	_ = binary.Write(&buf, le, struct {
		Size                    uint32
		Width, Height           int32
		Planes, BitCount        uint16
		Compression, ImageSize  uint32
		XPPM, YPPM              int32
		ColorsUsed, ColorsImpor uint32
	}{40, int32(s), int32(s * 2), 1, 32, 0, uint32(s*s*4 + maskStride*s), 0, 0, 0, 0})
	for y := s - 1; y >= 0; y-- {
		for x := 0; x < s; x++ {
			c := img.NRGBAAt(x, y)
			buf.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	buf.Write(make([]byte, maskStride*s)) // AND mask unused with alpha
	return buf.Bytes()
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
