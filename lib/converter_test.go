package lib

import (
	"fmt"
	"image"
	"image/color"
	"math/rand"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var (
	defaultPathRE = regexp.MustCompile(`<path stroke="#([0-9A-F]{6})" d="([^"]*)"/>`)
	mergedPathRE  = regexp.MustCompile(`<path fill="#([0-9A-F]{6})" d="([^"]*)"/>`)

	defaultCommandRE = regexp.MustCompile(`M(\d+) (\d+)h(\d+)`)
	mergedCommandRE  = regexp.MustCompile(`M(\d+) (-?\d+\.5)h(\d+)v(\d+)h-(\d+)z`)
)

type pixelKey struct {
	x int
	y int
}

// rasterizeDefault reconstructs the set of painted pixels from the default
// (stroked) SVG output.
func rasterizeDefault(t *testing.T, svg string) map[pixelKey]uint32 {
	t.Helper()
	pixels := make(map[pixelKey]uint32)
	for _, path := range defaultPathRE.FindAllStringSubmatch(svg, -1) {
		pixel, err := strconv.ParseUint(path[1], 16, 32)
		if err != nil {
			t.Fatalf("invalid color %q: %v", path[1], err)
		}
		commands := defaultCommandRE.FindAllStringSubmatch(path[2], -1)
		if len(commands) == 0 {
			t.Fatalf("path for #%s has no commands: %q", path[1], path[2])
		}
		for _, command := range commands {
			x, _ := strconv.Atoi(command[1])
			y, _ := strconv.Atoi(command[2])
			length, _ := strconv.Atoi(command[3])
			for i := 0; i < length; i++ {
				assertPixel(t, pixels, pixelKey{x + i, y}, uint32(pixel))
			}
		}
	}
	return pixels
}

// rasterizeMerged reconstructs the set of painted pixels from the merged
// (filled rectangle) SVG output.
func rasterizeMerged(t *testing.T, svg string) map[pixelKey]uint32 {
	t.Helper()
	pixels := make(map[pixelKey]uint32)
	for _, path := range mergedPathRE.FindAllStringSubmatch(svg, -1) {
		pixel, err := strconv.ParseUint(path[1], 16, 32)
		if err != nil {
			t.Fatalf("invalid color %q: %v", path[1], err)
		}
		commands := mergedCommandRE.FindAllStringSubmatch(path[2], -1)
		if len(commands) == 0 {
			t.Fatalf("path for #%s has no commands: %q", path[1], path[2])
		}
		for _, command := range commands {
			x, _ := strconv.Atoi(command[1])
			yTop, err := strconv.ParseFloat(command[2], 64)
			if err != nil {
				t.Fatalf("invalid y %q: %v", command[2], err)
			}
			y := int(yTop + 0.5)
			width, _ := strconv.Atoi(command[3])
			height, _ := strconv.Atoi(command[4])
			backWidth, _ := strconv.Atoi(command[5])
			if backWidth != width {
				t.Fatalf("rectangle is not closed: width %d, back width %d", width, backWidth)
			}
			if height < 1 || width < 1 {
				t.Fatalf("empty rectangle: %dx%d", width, height)
			}
			for dy := 0; dy < height; dy++ {
				for dx := 0; dx < width; dx++ {
					assertPixel(t, pixels, pixelKey{x + dx, y + dy}, uint32(pixel))
				}
			}
		}
	}
	return pixels
}

func assertPixel(t *testing.T, pixels map[pixelKey]uint32, key pixelKey, value uint32) {
	t.Helper()
	if previous, ok := pixels[key]; ok {
		t.Fatalf("pixel %v painted twice (0x%06X then 0x%06X)", key, previous, value)
	}
	pixels[key] = value
}

// countNodes reports XML element count and the number of geometry commands
// ("M" subpaths), which is what makes editors slow on large exports.
func countNodes(svg string) (elements int, commands int) {
	elements = strings.Count(svg, "<path ")
	elements += strings.Count(svg, "<rect ")
	commands = strings.Count(svg, "M")
	return elements, commands
}

// reference implementation copied from the original lib/converter.go. The
// original iterates a Go map, so its color order is nondeterministic; this
// reference sorts colors so it can be compared exactly. Per-color geometry is
// byte-for-byte identical to the original.
func referenceConvert(img image.Image, transparent color.Color) string {
	transparentValue := colorToInt(transparent)
	size := img.Bounds().Size()
	data := referenceGetColorData(img)
	delete(data, transparentValue)

	colors := make([]uint32, 0, len(data))
	for pixel := range data {
		colors = append(colors, pixel)
	}
	sort.Slice(colors, func(i, j int) bool { return colors[i] < colors[j] })

	paths := make([]string, 0, len(colors))
	for _, pixel := range colors {
		paths = append(paths, referencePath(pixel, data[pixel]))
	}

	return fmt.Sprintf(`
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 -0.5 %d %d" shape-rendering="crispEdges">
%s
</svg>`,
		size.X, size.Y, strings.Join(paths, "\n"))
}

func referencePath(pixel uint32, points []image.Point) string {
	pointsData := make([]string, 0)
	start := image.Point{X: -1, Y: -1}
	l := 0

	for _, point := range points {
		if start.X < 0 {
			start = point
			l = 1
			continue
		}
		if point.Y > start.Y || start.X+l != point.X {
			pointsData = append(pointsData, referencePathData(start, l))
			start = point
			l = 1
			continue
		}
		l++
	}

	if start.X >= 0 && l > 0 {
		pointsData = append(pointsData, referencePathData(start, l))
	}

	return fmt.Sprintf(`<path stroke="#%06X" d="%s"/>`, pixel, strings.Join(pointsData, ""))
}

func referencePathData(point image.Point, len int) string {
	return fmt.Sprintf("M%d %dh%d", point.X, point.Y, len)
}

func referenceGetColorData(img image.Image) map[uint32][]image.Point {
	result := map[uint32][]image.Point{}
	bounds := img.Bounds()
	size := bounds.Size()
	for y := 0; y < size.Y; y++ {
		for x := 0; x < size.X; x++ {
			pixel := img.At(x, y)
			value := colorToInt(pixel)

			points, ok := result[value]
			if !ok {
				points = make([]image.Point, 0)
			}
			points = append(points, image.Point{X: x, Y: y})

			result[value] = points
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Test images (always zero-based unless a test says otherwise)
// ---------------------------------------------------------------------------

var (
	red    = color.NRGBA{R: 255, A: 255}
	green  = color.NRGBA{G: 255, A: 255}
	blue   = color.NRGBA{B: 255, A: 255}
	purple = color.NRGBA{R: 255, B: 255, A: 255}
)

// newPattern builds a w*h image; colorAt decides the color of each pixel.
func newPattern(width, height int, colorAt func(x, y int) color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, colorAt(x, y))
		}
	}
	return img
}

func checkerboard(width, height, block int) *image.NRGBA {
	return newPattern(width, height, func(x, y int) color.NRGBA {
		if (x/block+y/block)%2 == 0 {
			return red
		}
		return blue
	})
}

func verticalStripes(width, height, stripe int) *image.NRGBA {
	return newPattern(width, height, func(x, y int) color.NRGBA {
		if (x/stripe)%2 == 0 {
			return green
		}
		return blue
	})
}

func horizontalStripes(width, height, stripe int) *image.NRGBA {
	return newPattern(width, height, func(x, y int) color.NRGBA {
		if (y/stripe)%2 == 0 {
			return green
		}
		return blue
	})
}

func semiTransparent(width, height int) *image.NRGBA {
	return newPattern(width, height, func(x, y int) color.NRGBA {
		switch (x/3 + y/3) % 3 {
		case 0:
			return color.NRGBA{R: 200, A: 255}
		case 1:
			return color.NRGBA{R: 200, A: 128}
		default:
			return color.NRGBA{R: 200, G: 60, A: 255}
		}
	})
}

func randomImage(width, height int, seed int64) *image.NRGBA {
	rng := rand.New(rand.NewSource(seed))
	palette := []color.NRGBA{red, green, blue, purple}
	return newPattern(width, height, func(x, y int) color.NRGBA {
		return palette[rng.Intn(len(palette))]
	})
}

func allTestImages() map[string]image.Image {
	return map[string]image.Image{
		"checkerboard-1px":     checkerboard(16, 16, 1),
		"checkerboard-4px":     checkerboard(16, 16, 4),
		"checkerboard-3px-odd": checkerboard(17, 13, 3),
		"vertical-stripes":     verticalStripes(17, 11, 2),
		"horizontal-stripes":   horizontalStripes(17, 11, 2),
		"semi-transparent":     semiTransparent(19, 14),
		"solid":                newPattern(9, 7, func(x, y int) color.NRGBA { return green }),
		"single-pixel":         newPattern(1, 1, func(x, y int) color.NRGBA { return red }),
		"random-23x17":         randomImage(23, 17, 1),
		"random-64x64":         randomImage(64, 64, 2),
		"width-zero":           newPattern(0, 5, func(x, y int) color.NRGBA { return red }),
		"height-zero":          newPattern(5, 0, func(x, y int) color.NRGBA { return red }),
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestDefaultModeMatchesOriginalImplementation(t *testing.T) {
	for name, img := range allTestImages() {
		t.Run(name, func(t *testing.T) {
			want := referenceConvert(img, color.NRGBA{R: 255, B: 255, A: 255})
			got := Convert(img, color.NRGBA{R: 255, B: 255, A: 255})
			if got != want {
				t.Fatalf("default output differs from original implementation\n--- want ---\n%s\n--- got ---\n%s", want, got)
			}
		})
	}
}

func TestMergePreservesPaintedPixels(t *testing.T) {
	transparent := color.NRGBA{R: 255, B: 255, A: 255}
	for name, img := range allTestImages() {
		t.Run(name, func(t *testing.T) {
			plain := Convert(img, transparent)
			merged := Convert(img, transparent, Options{Merge: true})

			plainPixels := rasterizeDefault(t, plain)
			mergedPixels := rasterizeMerged(t, merged)

			if len(plainPixels) != len(mergedPixels) {
				t.Fatalf("pixel count differs: default %d, merged %d", len(plainPixels), len(mergedPixels))
			}
			for key, pixel := range plainPixels {
				other, ok := mergedPixels[key]
				if !ok {
					t.Fatalf("merged output misses pixel %v (#%06X)", key, pixel)
				}
				if other != pixel {
					t.Fatalf("pixel %v color differs: default #%06X, merged #%06X", key, pixel, other)
				}
			}
		})
	}
}

func TestMergeReducesGeometryCommands(t *testing.T) {
	transparent := color.NRGBA{R: 255, B: 255, A: 255}

	tests := []struct {
		name     string
		img      image.Image
		wantLess bool
	}{
		{"solid-9x7", newPattern(9, 7, func(x, y int) color.NRGBA { return green }), true},
		{"vertical-stripes", verticalStripes(17, 11, 2), true},
		{"checkerboard-4px", checkerboard(16, 16, 4), true},
		{"horizontal-stripes", horizontalStripes(17, 11, 2), true},
		{"checkerboard-1px", checkerboard(16, 16, 1), false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, plainCommands := countNodes(Convert(test.img, transparent))
			_, mergedCommands := countNodes(Convert(test.img, transparent, Options{Merge: true}))

			if mergedCommands > plainCommands {
				t.Fatalf("merge increased commands: default %d, merged %d", plainCommands, mergedCommands)
			}
			if test.wantLess && mergedCommands >= plainCommands {
				t.Fatalf("merge did not reduce commands: default %d, merged %d", plainCommands, mergedCommands)
			}
			if !test.wantLess && mergedCommands != plainCommands {
				t.Fatalf("expected no possible merge: default %d, merged %d", plainCommands, mergedCommands)
			}

			// XML elements must not increase: one <path> per color either way.
			plainElements, _ := countNodes(Convert(test.img, transparent))
			mergedElements, _ := countNodes(Convert(test.img, transparent, Options{Merge: true}))
			if mergedElements > plainElements {
				t.Fatalf("merge increased XML nodes: default %d, merged %d", plainElements, mergedElements)
			}
		})
	}

	// A 7-row solid image must collapse to a single rectangle command.
	_, commands := countNodes(Convert(newPattern(9, 7, func(x, y int) color.NRGBA { return green }), transparent, Options{Merge: true}))
	if commands != 1 {
		t.Fatalf("solid image should merge to one rectangle, got %d commands", commands)
	}

	// Vertical stripes of width 2, 11 rows and 17 columns: 9 stripes, each a
	// single full-height rectangle.
	_, commands = countNodes(Convert(verticalStripes(17, 11, 2), transparent, Options{Merge: true}))
	if commands != 9 {
		t.Fatalf("vertical stripes should merge to 9 rectangles, got %d", commands)
	}
}

func TestMergeIsDeterministic(t *testing.T) {
	img := randomImage(40, 31, 7)
	transparent := color.NRGBA{R: 255, B: 255, A: 255}

	for _, options := range []Options{{}, {Merge: true}} {
		want := Convert(img, transparent, options)
		for i := 0; i < 50; i++ {
			if got := Convert(img, transparent, options); got != want {
				t.Fatalf("conversion %d not reproducible (Merge=%v)", i, options.Merge)
			}
		}
	}
}

func TestNonZeroBoundsNormalized(t *testing.T) {
	transparent := color.NRGBA{R: 255, B: 255, A: 255}

	// Build a zero-based image first, then embed it in a larger RGBA whose
	// bounds start at a non-zero point.
	base := checkerboard(11, 9, 2)
	offset := image.NewNRGBA(image.Rect(5, 7, 5+11, 7+9))
	for y := 0; y < 9; y++ {
		for x := 0; x < 11; x++ {
			offset.SetNRGBA(5+x, 7+y, base.NRGBAAt(x, y))
		}
	}

	wantDefault := Convert(base, transparent)
	wantMerged := Convert(base, transparent, Options{Merge: true})
	if got := Convert(offset, transparent); got != wantDefault {
		t.Fatalf("default output depends on bounds.Min:\n--- want ---\n%s\n--- got ---\n%s", wantDefault, got)
	}
	if got := Convert(offset, transparent, Options{Merge: true}); got != wantMerged {
		t.Fatalf("merged output depends on bounds.Min:\n--- want ---\n%s\n--- got ---\n%s", wantMerged, got)
	}

	// SubImage has non-zero Min too.
	sub := image.NewRGBA(image.Rect(0, 0, 20, 20))
	subed := sub.SubImage(image.Rect(3, 4, 10, 12))
	svg := Convert(subed, transparent)
	if !strings.Contains(svg, "viewBox=\"0 -0.5 7 8\"") {
		t.Fatalf("subimage viewBox is wrong: %s", svg)
	}
}

func TestTransparentAndAlphaRules(t *testing.T) {
	transparent := color.NRGBA{R: 255, B: 255, A: 255}

	img := newPattern(4, 1, func(x, y int) color.NRGBA {
		switch x {
		case 0:
			return purple // opaque transparent color: removed
		case 1:
			return color.NRGBA{R: 255, B: 255, A: 128} // semi-transparent purple: kept
		case 2:
			return color.NRGBA{R: 255, A: 255} // opaque red
		default:
			return color.NRGBA{R: 0, A: 0} // fully transparent black
		}
	})

	svg := Convert(img, transparent)
	pixels := rasterizeDefault(t, svg)

	if _, ok := pixels[pixelKey{0, 0}]; ok {
		t.Fatalf("opaque transparent color must be removed")
	}
	if pixel := pixels[pixelKey{1, 0}]; pixel != 0x800080 {
		t.Fatalf("semi-transparent purple should keep its premultiplied key 0x800080, got #%06X", pixel)
	}
	if pixel := pixels[pixelKey{2, 0}]; pixel != 0xFF0000 {
		t.Fatalf("opaque red should be #FF0000, got #%06X", pixel)
	}
	if pixel := pixels[pixelKey{3, 0}]; pixel != 0x000000 {
		t.Fatalf("transparent black should be #000000, got #%06X", pixel)
	}

	// Merge mode must apply exactly the same rules.
	mergedPixels := rasterizeMerged(t, Convert(img, transparent, Options{Merge: true}))
	if len(mergedPixels) != len(pixels) {
		t.Fatalf("merge changed the transparent/alpha pixel set")
	}
	for key, pixel := range pixels {
		if mergedPixels[key] != pixel {
			t.Fatalf("merge changed pixel %v: #%06X -> #%06X", key, pixel, mergedPixels[key])
		}
	}
}

func TestPalettedImage(t *testing.T) {
	palette := color.Palette{
		color.NRGBA{R: 255, B: 255, A: 255}, // transparent entry
		color.NRGBA{R: 10, G: 20, B: 30, A: 255},
		color.NRGBA{R: 40, G: 50, B: 60, A: 255},
	}
	img := image.NewPaletted(image.Rect(0, 0, 6, 6), palette)
	for y := 0; y < 6; y++ {
		for x := 0; x < 6; x++ {
			img.SetColorIndex(x, y, uint8((x/2+y/2)%3))
		}
	}

	transparent := color.NRGBA{R: 255, B: 255, A: 255}
	plain := rasterizeDefault(t, Convert(img, transparent))
	merged := rasterizeMerged(t, Convert(img, transparent, Options{Merge: true}))

	if len(plain) != len(merged) {
		t.Fatalf("paletted image pixel count differs: %d vs %d", len(plain), len(merged))
	}
	for key, pixel := range plain {
		if merged[key] != pixel {
			t.Fatalf("paletted pixel %v differs", key)
		}
	}
	if _, ok := plain[pixelKey{0, 0}]; ok {
		t.Fatalf("palette transparent entry was not removed")
	}
}

func TestEmptyAndNilImages(t *testing.T) {
	transparent := color.NRGBA{R: 255, B: 255, A: 255}

	cases := map[string]struct {
		img  image.Image
		size string
	}{
		"empty":       {image.NewNRGBA(image.Rect(0, 0, 0, 0)), "0 -0.5 0 0"},
		"zero-width":  {image.NewNRGBA(image.Rect(0, 0, 0, 10)), "0 -0.5 0 10"},
		"zero-height": {image.NewNRGBA(image.Rect(0, 0, 10, 0)), "0 -0.5 10 0"},
		"nil":         {nil, "0 -0.5 0 0"},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			for _, options := range []Options{{}, {Merge: true}} {
				svg := Convert(test.img, transparent, options)
				if !strings.HasPrefix(svg, "\n<svg ") {
					t.Fatalf("missing svg header: %q", svg)
				}
				if strings.Contains(svg, "<path ") {
					t.Fatalf("empty image produced paths: %s", svg)
				}
			}
			want := "viewBox=\"" + test.size + "\""
			if !strings.Contains(Convert(test.img, transparent), want) {
				t.Fatalf("empty image viewBox should contain %q", want)
			}
		})
	}
}

func TestLargeImage(t *testing.T) {
	// 4096x4096 = 16.7M pixels. A uniform image has one run per row, and merge
	// collapses it into a single rectangle.
	img := newPattern(4096, 4096, func(x, y int) color.NRGBA { return green })
	transparent := color.NRGBA{R: 255, B: 255, A: 255}

	plain := Convert(img, transparent)
	if got := strings.Count(plain, "M"); got != 4096 {
		t.Fatalf("uniform large image should have one run per row, got %d", got)
	}

	merged := Convert(img, transparent, Options{Merge: true})
	if got := strings.Count(merged, "M"); got != 1 {
		t.Fatalf("uniform large image should merge to one rectangle, got %d", got)
	}
	if !strings.Contains(merged, `viewBox="0 -0.5 4096 4096"`) {
		t.Fatalf("large image viewBox is wrong")
	}

	// Odd-sized huge bounds must not overflow or lose the last row/column.
	odd := newPattern(4001, 3001, func(x, y int) color.NRGBA { return blue })
	merged = Convert(odd, transparent, Options{Merge: true})
	if !strings.Contains(merged, `viewBox="0 -0.5 4001 3001"`) {
		t.Fatalf("odd large image viewBox is wrong")
	}
}

func TestMergeAcrossTransparentGap(t *testing.T) {
	transparent := color.NRGBA{R: 255, B: 255, A: 255}

	// Two identical runs separated by a transparent pixel must stay separate,
	// because the transparent pixel is not painted.
	img := newPattern(6, 1, func(x, y int) color.NRGBA {
		if x == 2 || x == 3 {
			return transparent
		}
		return red
	})

	merged := Convert(img, transparent, Options{Merge: true})
	pixels := rasterizeMerged(t, merged)
	if len(pixels) != 4 {
		t.Fatalf("expected 4 painted pixels, got %d", len(pixels))
	}
	if len(merged) == 0 || strings.Count(merged, "M") != 2 {
		t.Fatalf("transparent gap should keep two separate runs:\n%s", merged)
	}
}

func TestMergeOnlyStacksConsecutiveRows(t *testing.T) {
	transparent := color.NRGBA{R: 255, B: 255, A: 255}

	// Column x=1 is painted on rows 0 and 2, but not row 1: no rectangle may
	// span the hole.
	img := newPattern(3, 3, func(x, y int) color.NRGBA {
		if x == 1 && y == 1 {
			return transparent
		}
		return red
	})

	merged := Convert(img, transparent, Options{Merge: true})
	for _, command := range mergedCommandRE.FindAllStringSubmatch(merged, -1) {
		height, _ := strconv.Atoi(command[4])
		if height > 2 {
			t.Fatalf("rectangle spans a gap: %s", command[0])
		}
	}
}

// TestMergePropertyRandomImages checks the core invariant over many random
// images of varying sizes: merge mode paints exactly the same pixels as the
// default mode.
func TestMergePropertyRandomImages(t *testing.T) {
	transparent := color.NRGBA{R: 255, B: 255, A: 255}
	rng := rand.New(rand.NewSource(20261121))

	for iteration := 0; iteration < 100; iteration++ {
		width := rng.Intn(30)
		height := rng.Intn(30)
		img := newPattern(width, height, func(x, y int) color.NRGBA {
			switch rng.Intn(5) {
			case 0:
				return transparent
			case 1:
				return color.NRGBA{R: 200, A: 128}
			default:
				return color.NRGBA{R: uint8(rng.Intn(3) * 100), G: uint8(rng.Intn(2) * 100), A: 255}
			}
		})

		plain := rasterizeDefault(t, Convert(img, transparent))
		merged := rasterizeMerged(t, Convert(img, transparent, Options{Merge: true}))

		if len(plain) != len(merged) {
			t.Fatalf("iteration %d (%dx%d): pixel count default %d, merged %d", iteration, width, height, len(plain), len(merged))
		}
		for key, pixel := range plain {
			if merged[key] != pixel {
				t.Fatalf("iteration %d (%dx%d): pixel %v differs", iteration, width, height, key)
			}
		}
	}
}
