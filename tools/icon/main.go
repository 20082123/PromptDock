// Generates the original PromptDock icon and a deterministic AMD64 COFF
// resource object, using only Go's standard library. No downloaded artwork.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func u16(b []byte, i int, v uint16) { binary.LittleEndian.PutUint16(b[i:], v) }
func u32(b []byte, i int, v uint32) { binary.LittleEndian.PutUint32(b[i:], v) }
func glyph(x, y float64) bool {
	// A white prompt chevron and cursor in a blue dock tile.
	dx := x - .34
	dy := y - .43
	chevron := dx >= -.10 && dx <= .08 && (dy-dx < .047 && dy-dx > -.047 || dy+dx < .047 && dy+dx > -.047)
	cursor := x > .53 && x < .75 && y > .55 && y < .61
	return chevron || cursor
}
func iconPNG(size int) []byte {
	im := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a uint32
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					fx := (float64(x) + (float64(sx)+.5)/4) / float64(size)
					fy := (float64(y) + (float64(sy)+.5)/4) / float64(size)
					dx := max(.18-fx, 0, fx-.82)
					dy := max(.18-fy, 0, fy-.82)
					if fx < .04 || fx > .96 || fy < .04 || fy > .96 || dx*dx+dy*dy > .14*.14 {
						continue
					}
					cr, cg, cb := uint32(36), uint32(99), uint32(221)
					if glyph(fx, fy) {
						cr, cg, cb = 255, 255, 255
					}
					r += cr
					g += cg
					b += cb
					a += 255
				}
			}
			if a > 0 {
				im.SetNRGBA(x, y, color.NRGBA{uint8(r * 255 / a), uint8(g * 255 / a), uint8(b * 255 / a), uint8(a / 16)})
			}
		}
	}
	var out bytes.Buffer
	must(png.Encode(&out, im))
	return out.Bytes()
}

type leaf struct {
	id      int
	data    []byte
	entry   int
	payload int
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	must(os.MkdirAll(filepath.Join(root, "assets"), 0755))
	sizes := []int{16, 20, 24, 32, 48, 64, 128, 256}
	leaves := make([]leaf, len(sizes)+2)
	ico := make([]byte, 6+16*len(sizes))
	u16(ico, 2, 1)
	u16(ico, 4, uint16(len(sizes)))
	group := make([]byte, 6+14*len(sizes))
	u16(group, 2, 1)
	u16(group, 4, uint16(len(sizes)))
	for i, size := range sizes {
		data := iconPNG(size)
		leaves[i] = leaf{id: i + 1, data: data}
		p := 6 + i*16
		ico[p] = byte(size % 256)
		ico[p+1] = byte(size % 256)
		u16(ico, p+4, 1)
		u16(ico, p+6, 32)
		u32(ico, p+8, uint32(len(data)))
		u32(ico, p+12, uint32(len(ico)))
		ico = append(ico, data...)
		q := 6 + i*14
		copy(group[q:q+12], ico[p:p+12])
		u16(group, q+12, uint16(i+1))
	}
	leaves[len(sizes)] = leaf{id: 101, data: group}
	manifest, err := os.ReadFile(filepath.Join(root, "assets", "app.manifest"))
	must(err)
	leaves[len(sizes)+1] = leaf{id: 1, data: manifest}
	// Resource tree: type -> integer id -> language -> data entry.
	iconType := 40
	groupType := iconType + 16 + 8*len(sizes)
	manifestType := groupType + 24
	langBase := manifestType + 24
	dataBase := langBase + 24*len(leaves)
	payloadBase := dataBase + 16*len(leaves)
	res := make([]byte, payloadBase)
	u16(res, 14, 3)
	u32(res, 16, 3)
	u32(res, 20, uint32(iconType)|0x80000000)
	u32(res, 24, 14)
	u32(res, 28, uint32(groupType)|0x80000000)
	u32(res, 32, 24)
	u32(res, 36, uint32(manifestType)|0x80000000)
	u16(res, iconType+14, uint16(len(sizes)))
	u16(res, groupType+14, 1)
	u16(res, manifestType+14, 1)
	for i := range leaves {
		lang := langBase + 24*i
		entry := dataBase + 16*i
		leaves[i].entry = entry
		leaves[i].payload = len(res)
		typeEntry := iconType + 16 + 8*i
		if i == len(sizes) {
			typeEntry = groupType + 16
		}
		if i == len(sizes)+1 {
			typeEntry = manifestType + 16
		}
		u32(res, typeEntry, uint32(leaves[i].id))
		u32(res, typeEntry+4, uint32(lang)|0x80000000)
		u16(res, lang+14, 1)
		u32(res, lang+16, 1033)
		u32(res, lang+20, uint32(entry))
		u32(res, entry, uint32(len(res)))
		u32(res, entry+4, uint32(len(leaves[i].data)))
		res = append(res, leaves[i].data...)
		for len(res)%4 != 0 {
			res = append(res, 0)
		}
	}
	coff := make([]byte, 60)
	u16(coff, 0, 0x8664)
	u16(coff, 2, 1)
	u32(coff, 8, uint32(60+len(res)+10*len(leaves)))
	u32(coff, 12, 1)
	copy(coff[20:28], []byte(".rsrc"))
	u32(coff, 36, uint32(len(res)))
	u32(coff, 40, 60)
	u32(coff, 44, uint32(60+len(res)))
	u16(coff, 52, uint16(len(leaves)))
	u32(coff, 56, 0x40000040)
	coff = append(coff, res...)
	for _, l := range leaves {
		rel := make([]byte, 10)
		u32(rel, 0, uint32(l.entry))
		u16(rel, 8, 3)
		coff = append(coff, rel...)
	}
	sym := make([]byte, 18)
	copy(sym, []byte(".rsrc"))
	u16(sym, 12, 1)
	sym[16] = 3
	coff = append(coff, sym...)
	coff = append(coff, 4, 0, 0, 0)
	must(os.WriteFile(filepath.Join(root, "assets", "PromptDock.ico"), ico, 0644))
	must(os.WriteFile(filepath.Join(root, "assets", "icon.png"), iconPNG(256), 0644))
	must(os.WriteFile(filepath.Join(root, "app_windows_amd64.syso"), coff, 0644))
	must(os.MkdirAll(filepath.Join(root, "cmd", "uninstall"), 0755))
	must(os.WriteFile(filepath.Join(root, "cmd", "uninstall", "app_windows_amd64.syso"), coff, 0644))
}
