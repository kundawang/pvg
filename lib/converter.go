package lib

import (
	"fmt"
	"image"
	"image/color"
	"sort"
	"strconv"
	"strings"
)

// Options controls how an image is rendered.
type Options struct {
	// Merge enables "same-color merge" mode.
	//
	// In the default mode every horizontal run of equally colored pixels is
	// emitted as a stroked path segment ("M x y h length"). In merge mode the
	// same horizontal runs are built first, and runs that have exactly the same
	// span (same X and the same length) on consecutive rows are stacked into a
	// single filled rectangle. Only the SVG representation changes: the covered
	// pixels are identical to the default mode.
	Merge bool
}

// run is a horizontal run of equally colored pixels inside one row.
type run struct {
	x      int
	y      int
	length int
}

// rect is a merged run: width pixels wide and height rows tall.
type rect struct {
	x      int
	y      int
	width  int
	height int
}

// Convert renders img as a SVG without tracing. Pixels whose (premultiplied)
// RGB value equals transparent are skipped. The output order is deterministic:
// colors are emitted in ascending RGB order and runs/rectangles in scan order.
//
// An optional Options may be supplied; only the first one is used. Keeping the
// parameter variadic preserves the original two-argument signature:
//
//	lib.Convert(img, transparent)
//	lib.Convert(img, transparent, lib.Options{Merge: true})
func Convert(img image.Image, transparent color.Color, options ...Options) string {
	option := Options{}
	if len(options) > 0 {
		option = options[0]
	}
	return ConvertWithOptions(img, transparent, option)
}

// ConvertWithOptions is Convert with explicit rendering options.
func ConvertWithOptions(img image.Image, transparent color.Color, options Options) string {
	width, height := 0, 0
	runsByColor := make(map[uint32][]run)

	if img != nil {
		bounds := img.Bounds()
		width, height = bounds.Dx(), bounds.Dy()
		if width > 0 && height > 0 {
			collectRuns(img, bounds, colorToInt(transparent), runsByColor)
		}
	}

	// Map iteration is randomized in Go. Sorting the color keys keeps repeated
	// conversions byte-for-byte identical, including batch/parallel runs.
	colors := make([]uint32, 0, len(runsByColor))
	for pixel := range runsByColor {
		colors = append(colors, pixel)
	}
	sort.Slice(colors, func(i, j int) bool { return colors[i] < colors[j] })

	paths := make([]string, 0, len(colors))
	for _, pixel := range colors {
		if options.Merge {
			if merged := mergeRuns(runsByColor[pixel]); len(merged) > 0 {
				paths = append(paths, rectsPath(pixel, merged))
			}
		} else {
			paths = append(paths, runsPath(pixel, runsByColor[pixel]))
		}
	}

	return fmt.Sprintf(`
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 -0.5 %d %d" shape-rendering="crispEdges">
%s
</svg>`,
		width, height, strings.Join(paths, "\n"))
}

// collectRuns scans img in row-major order and appends horizontal runs to out.
// Coordinates are normalized to the image bounds, so an image with a non-zero
// bounds.Min produces the same SVG as its zero-based equivalent. Pixels equal
// to the transparent value are omitted and terminate the current run, exactly
// like the historical point-list implementation.
//
// Memory use is O(number of runs), not O(number of pixels).
func collectRuns(img image.Image, bounds image.Rectangle, transparent uint32, out map[uint32][]run) {
	width := bounds.Dx()
	height := bounds.Dy()

	for y := 0; y < height; y++ {
		current := uint32(0)
		start := 0
		length := 0

		for x := 0; x < width; x++ {
			value := colorToInt(img.At(bounds.Min.X+x, bounds.Min.Y+y))

			if length == 0 {
				if value == transparent {
					continue
				}
				current, start, length = value, x, 1
				continue
			}

			if value == current {
				length++
				continue
			}

			out[current] = append(out[current], run{x: start, y: y, length: length})
			if value == transparent {
				length = 0
			} else {
				current, start, length = value, x, 1
			}
		}

		if length > 0 {
			out[current] = append(out[current], run{x: start, y: y, length: length})
		}
	}
}

// runsPath renders the default (per-run) representation. It matches the
// historical output: one stroked path per color, with one "M x y h length"
// command per horizontal run.
func runsPath(pixel uint32, runs []run) string {
	pointsData := make([]string, 0, len(runs))
	for _, point := range runs {
		pointsData = append(pointsData, pathData(image.Point{X: point.x, Y: point.y}, point.length))
	}
	return fmt.Sprintf(`<path stroke="#%06X" d="%s"/>`, pixel, strings.Join(pointsData, ""))
}

// mergeRuns stacks runs with an identical span (same x and length) on
// consecutive rows into rectangles. Runs are supplied in scan order (top to
// bottom, left to right), so a rectangle is extended whenever the same span was
// seen on the immediately preceding row. A gap (any other run in between, or a
// transparent pixel in a different row) starts a new rectangle.
func mergeRuns(runs []run) []rect {
	type span struct {
		x      int
		length int
	}

	type openRect struct {
		index int
		lastY int
	}

	rects := make([]rect, 0, len(runs))
	open := make(map[span]openRect)

	for _, r := range runs {
		key := span{x: r.x, length: r.length}

		if previous, ok := open[key]; ok && previous.lastY == r.y-1 {
			rects[previous.index].height++
			previous.lastY = r.y
			open[key] = previous
			continue
		}

		rects = append(rects, rect{x: r.x, y: r.y, width: r.length, height: 1})
		open[key] = openRect{index: len(rects) - 1, lastY: r.y}
	}

	return rects
}

// rectsPath renders merged rectangles as a single filled path per color. Each
// rectangle is a closed subpath, so node count is one element per color while
// the number of subpaths drops from one-per-run to one-per-merged-rectangle.
// Filling (rather than stroking) avoids seams between stacked rectangles.
//
// A rectangle covering rows y..y+h-1 is drawn from y-0.5 to y-0.5+h, matching
// the pixel grid used by the stroked output and the "0 -0.5" viewBox. X and
// width need no half-pixel offset because runs start on a pixel boundary.
func rectsPath(pixel uint32, rects []rect) string {
	rectData := make([]string, 0, len(rects))
	for _, r := range rects {
		rectData = append(rectData, fmt.Sprintf("M%d %sh%dv%dh-%dz",
			r.x, halfPixel(r.y), r.width, r.height, r.width))
	}
	return fmt.Sprintf(`<path fill="#%06X" d="%s"/>`, pixel, strings.Join(rectData, ""))
}

// halfPixel returns y-0.5 in a locale-independent form ("-0.5", "0.5", "1.5").
func halfPixel(y int) string {
	if y == 0 {
		return "-0.5"
	}
	return strconv.Itoa(y-1) + ".5"
}

func pathData(point image.Point, len int) string {
	return fmt.Sprintf("M%d %dh%d", point.X, point.Y, len)
}

// colorToInt is kept identical to the original converter. It intentionally
// ignores alpha when forming the color key (RGBA() returns premultiplied
// values, so alpha still influences semi-transparent pixels). The same value is
// used for transparent-color matching.
func colorToInt(color color.Color) uint32 {
	r, g, b, _ := color.RGBA()
	r = r >> 8
	g = g >> 8
	b = b >> 8
	return r<<16 | g<<8 | b
}
