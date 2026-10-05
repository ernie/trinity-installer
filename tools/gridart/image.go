package main

import (
	"image"

	"golang.org/x/image/draw"
)

// centerCrop returns the largest centered sub-image of src with aspect w:h.
func centerCrop(src image.Image, w, h int) image.Image {
	b := src.Bounds()
	cw, ch := b.Dx(), b.Dy()
	if cw*h > ch*w {
		cw = (ch*w + h/2) / h
	} else {
		ch = (cw*h + w/2) / w
	}
	x0 := b.Min.X + (b.Dx()-cw)/2
	y0 := b.Min.Y + (b.Dy()-ch)/2
	out := image.NewNRGBA(image.Rect(0, 0, cw, ch))
	draw.Draw(out, out.Bounds(), src, image.Pt(x0, y0), draw.Src)
	return out
}

func fit(src image.Image, w, h int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(out, out.Bounds(), src, src.Bounds(), draw.Over, nil)
	return out
}

// composite draws overlay scaled to widthFrac of dst's width, centered, with its bottom edge bottomFrac of the height above the bottom.
func composite(dst *image.NRGBA, overlay image.Image, widthFrac, bottomFrac float64) {
	db, ob := dst.Bounds(), overlay.Bounds()
	w := int(float64(db.Dx()) * widthFrac)
	h := w * ob.Dy() / ob.Dx()
	x0 := (db.Dx() - w) / 2
	y1 := db.Dy() - int(float64(db.Dy())*bottomFrac)
	r := image.Rect(x0, y1-h, x0+w, y1)
	draw.CatmullRom.Scale(dst, r, overlay, ob, draw.Over, nil)
}

// compositeRight is composite anchored lower-right: the right edge sits rightFrac of the width in from the right.
func compositeRight(dst *image.NRGBA, overlay image.Image, widthFrac, bottomFrac, rightFrac float64) {
	db, ob := dst.Bounds(), overlay.Bounds()
	w := int(float64(db.Dx()) * widthFrac)
	h := w * ob.Dy() / ob.Dx()
	x1 := db.Dx() - int(float64(db.Dx())*rightFrac)
	y1 := db.Dy() - int(float64(db.Dy())*bottomFrac)
	r := image.Rect(x1-w, y1-h, x1, y1)
	draw.CatmullRom.Scale(dst, r, overlay, ob, draw.Over, nil)
}
