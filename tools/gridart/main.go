package main

import (
	"flag"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

func load(p string) image.Image {
	f, err := os.Open(p)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		log.Fatalf("%s: %v", p, err)
	}
	return img
}

func save(dir, name string, img image.Image) {
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(f, img); err != nil {
		log.Fatal(err)
	}
	log.Println("wrote", name)
}

func main() {
	src := flag.String("src", "", "folder with the wallpapers, wordmark and icon (required)")
	out := flag.String("out", "assets/grid", "output folder")
	flag.Parse()
	if *src == "" {
		log.Fatal("-src is required")
	}
	os.MkdirAll(*out, 0o755)
	wordmark := load(filepath.Join(*src, "trinty-type.png"))

	capsule := fit(centerCrop(load(filepath.Join(*src, "trinity-wallpaper-portrait-orig.png")), 600, 900), 600, 900)
	composite(capsule, wordmark, 0.7, 0.08)
	save(*out, "capsule.png", capsule)

	wide := fit(centerCrop(load(filepath.Join(*src, "trinity-wallpaper-wqhd.png")), 920, 430), 920, 430)
	compositeRight(wide, wordmark, 0.30, 0.07, 0.04)
	save(*out, "wide.png", wide)

	save(*out, "hero.png", fit(centerCrop(load(filepath.Join(*src, "trinity-wallpaper-uw5k.png")), 1920, 620), 1920, 620))
	save(*out, "logo.png", wordmark)
	save(*out, "icon.png", fit(load(filepath.Join(*src, "icon.png")), 256, 256))
}
