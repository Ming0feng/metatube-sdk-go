package imageutil

import (
	"image"
	"image/draw"
)

// Watermark draws wmk onto src at the destination offset pt.
func Watermark(src image.Image, wmk image.Image, pt image.Point) image.Image {
	dst := image.NewNRGBA(image.Rect(0, 0,
		src.Bounds().Dx(), src.Bounds().Dy()))
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
	b := wmk.Bounds()
	draw.Draw(dst, image.Rectangle{Min: pt, Max: pt.Add(b.Size())}, wmk, b.Min, draw.Over)
	return dst
}
