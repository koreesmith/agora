package media

import (
	"encoding/binary"
	"image"
)

// exifOrientation reads the EXIF orientation tag from JPEG data.
// Returns 1 (upright, no transform needed) if the tag is absent or unreadable.
// EXIF values: 1=normal, 2=flipH, 3=180°, 4=flipV, 5=transpose, 6=90°CW, 7=transverse, 8=90°CCW
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			break
		}
		marker := data[i+1]
		// segLen includes the 2 length bytes but not the 2 marker bytes
		segLen := int(data[i+2])<<8 | int(data[i+3])
		if segLen < 2 {
			break
		}
		end := i + 2 + segLen
		if marker == 0xE1 && end <= len(data) { // APP1
			seg := data[i+4 : end]
			if len(seg) >= 6 && string(seg[:6]) == "Exif\x00\x00" {
				return tiffOrientation(seg[6:])
			}
		}
		if marker == 0xDA { // SOS — image data begins
			break
		}
		i += 2 + segLen
	}
	return 1
}

func tiffOrientation(b []byte) int {
	if len(b) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(b[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(b[2:4]) != 0x002A {
		return 1
	}
	ifd := int(order.Uint32(b[4:8]))
	if ifd+2 > len(b) {
		return 1
	}
	n := int(order.Uint16(b[ifd : ifd+2]))
	for j := 0; j < n; j++ {
		off := ifd + 2 + j*12
		if off+12 > len(b) {
			break
		}
		if order.Uint16(b[off:off+2]) == 0x0112 { // Orientation tag
			v := int(order.Uint16(b[off+8 : off+10]))
			if v >= 1 && v <= 8 {
				return v
			}
		}
	}
	return 1
}

// applyOrientation rotates/flips img to match the EXIF orientation value.
func applyOrientation(img image.Image, o int) image.Image {
	switch o {
	case 2:
		return flipH(img)
	case 3:
		return rotate180(img)
	case 4:
		return flipV(img)
	case 5:
		return flipH(rotate90CW(img))
	case 6:
		return rotate90CW(img)
	case 7:
		return flipH(rotate90CCW(img))
	case 8:
		return rotate90CCW(img)
	}
	return img
}

// rotate90CW returns a new image rotated 90° clockwise.
// new(x, y) = src(y, srcH-1-x), output dimensions are (srcH, srcW).
func rotate90CW(src image.Image) image.Image {
	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, srcH, srcW))
	for y := 0; y < srcW; y++ {
		for x := 0; x < srcH; x++ {
			dst.Set(x, y, src.At(b.Min.X+y, b.Min.Y+srcH-1-x))
		}
	}
	return dst
}

// rotate90CCW returns a new image rotated 90° counter-clockwise.
// new(x, y) = src(srcW-1-y, x), output dimensions are (srcH, srcW).
func rotate90CCW(src image.Image) image.Image {
	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, srcH, srcW))
	for y := 0; y < srcW; y++ {
		for x := 0; x < srcH; x++ {
			dst.Set(x, y, src.At(b.Min.X+srcW-1-y, b.Min.Y+x))
		}
	}
	return dst
}

// rotate180 returns a new image rotated 180°.
func rotate180(src image.Image) image.Image {
	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, srcW, srcH))
	for y := 0; y < srcH; y++ {
		for x := 0; x < srcW; x++ {
			dst.Set(x, y, src.At(b.Min.X+srcW-1-x, b.Min.Y+srcH-1-y))
		}
	}
	return dst
}

// flipH returns a new image mirrored horizontally.
func flipH(src image.Image) image.Image {
	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, srcW, srcH))
	for y := 0; y < srcH; y++ {
		for x := 0; x < srcW; x++ {
			dst.Set(x, y, src.At(b.Min.X+srcW-1-x, b.Min.Y+y))
		}
	}
	return dst
}

// flipV returns a new image mirrored vertically.
func flipV(src image.Image) image.Image {
	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, srcW, srcH))
	for y := 0; y < srcH; y++ {
		for x := 0; x < srcW; x++ {
			dst.Set(x, y, src.At(b.Min.X+x, b.Min.Y+srcH-1-y))
		}
	}
	return dst
}
