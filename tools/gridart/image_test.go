package main

import (
	"image"
	"image/color"
	"testing"
)

func solid(w, h int, c color.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestCenterCropKeepsAspect(t *testing.T) {
	src := solid(3440, 1440, color.White)
	got := centerCrop(src, 1920, 620)
	b := got.Bounds()
	// 3440x1440 cropped to 1920:620 keeps the full width: 3440 x 1111
	if b.Dx() != 3440 || b.Dy() != 1111 {
		t.Fatalf("%v", b)
	}
	got = centerCrop(solid(852, 1846, color.White), 600, 900)
	if b := got.Bounds(); b.Dx() != 852 || b.Dy() != 1278 {
		t.Fatalf("%v", b)
	}
}

func TestFitProducesExactSize(t *testing.T) {
	got := fit(solid(3440, 1111, color.White), 1920, 620)
	if b := got.Bounds(); b.Dx() != 1920 || b.Dy() != 620 {
		t.Fatalf("%v", b)
	}
}

func TestCompositePlacesOverlay(t *testing.T) {
	dst := solid(600, 900, color.Black)
	overlay := solid(100, 20, color.White)
	composite(dst, overlay, 0.7, 0.1)
	// 70% width = 420 wide, 84 tall, bottom edge at 90% of 900 = 810, centered: x 90..510, y 726..810
	if dst.NRGBAAt(300, 770) != (color.NRGBA{255, 255, 255, 255}) {
		t.Fatalf("center pixel %v", dst.NRGBAAt(300, 770))
	}
	if dst.NRGBAAt(300, 100) != (color.NRGBA{0, 0, 0, 255}) {
		t.Fatalf("top pixel changed %v", dst.NRGBAAt(300, 100))
	}
	if dst.NRGBAAt(50, 770) != (color.NRGBA{0, 0, 0, 255}) {
		t.Fatalf("left margin changed %v", dst.NRGBAAt(50, 770))
	}
}

func TestCompositeRightPlacesOverlay(t *testing.T) {
	dst := solid(920, 430, color.Black)
	overlay := solid(100, 20, color.White)
	compositeRight(dst, overlay, 0.30, 0.07, 0.04)
	// 276 x 55, right edge at 920-36=884 (int truncation of 36.8), bottom at 430-30=400
	if dst.NRGBAAt(750, 380) != (color.NRGBA{255, 255, 255, 255}) {
		t.Fatalf("overlay pixel %v", dst.NRGBAAt(750, 380))
	}
	if dst.NRGBAAt(300, 380) != (color.NRGBA{0, 0, 0, 255}) {
		t.Fatalf("left changed %v", dst.NRGBAAt(300, 380))
	}
	if dst.NRGBAAt(900, 380) != (color.NRGBA{0, 0, 0, 255}) {
		t.Fatalf("right margin changed %v", dst.NRGBAAt(900, 380))
	}
}
