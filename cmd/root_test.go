package cmd

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/devkanro/pvg/lib"
)

var testTransparent = color.NRGBA{R: 255, B: 255, A: 255}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
}

func pattern(width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			switch (x/2 + y/3) % 3 {
			case 0:
				img.SetNRGBA(x, y, color.NRGBA{R: 200, A: 255})
			case 1:
				img.SetNRGBA(x, y, color.NRGBA{G: 200, A: 255})
			default:
				img.SetNRGBA(x, y, color.NRGBA{R: 255, B: 255, A: 255})
			}
		}
	}
	return img
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

func TestParseHexColor(t *testing.T) {
	tests := map[string]color.RGBA{
		// Note the historical #AARRGGBB ordering for 9-digit values.
		"#FFFF00FF": {R: 0xFF, G: 0x00, B: 0xFF, A: 0xFF},
		"#123456":   {R: 0x12, G: 0x34, B: 0x56, A: 0xff},
		"#F0F":      {R: 0xff, G: 0, B: 0xff, A: 0xff},
		"#12345678": {R: 0x34, G: 0x56, B: 0x78, A: 0x12},
	}
	for input, want := range tests {
		got, err := parseHexColor(input)
		if err != nil {
			t.Fatalf("parseHexColor(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("parseHexColor(%q) = %#v, want %#v", input, got, want)
		}
	}

	for _, input := range []string{"", "#12", "red", "#GGGGGG"} {
		if _, err := parseHexColor(input); err == nil {
			t.Fatalf("parseHexColor(%q) should fail", input)
		}
	}
}

func TestResolveFileOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "sprite.png")
	writePNG(t, input, pattern(4, 4))

	t.Run("omitted-output-uses-svg-next-to-input", func(t *testing.T) {
		got, err := resolveFileOutput(input, input)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(dir, "sprite.svg"); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("existing-directory", func(t *testing.T) {
		out := filepath.Join(dir, "out")
		if err := os.MkdirAll(out, 0755); err != nil {
			t.Fatal(err)
		}
		got, err := resolveFileOutput(input, out)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(out, "sprite.svg"); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("svg-file", func(t *testing.T) {
		out := filepath.Join(dir, "custom.svg")
		got, err := resolveFileOutput(input, out)
		if err != nil {
			t.Fatal(err)
		}
		if got != out {
			t.Fatalf("got %q, want %q", got, out)
		}
	})

	t.Run("nonexistent-nonsvg-is-directory", func(t *testing.T) {
		out := filepath.Join(dir, "newdir")
		got, err := resolveFileOutput(input, out)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(out, "sprite.svg"); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("same-file-is-rejected", func(t *testing.T) {
		pngSprite := filepath.Join(dir, "same.png")
		writePNG(t, pngSprite, pattern(2, 2))

		// Omitting the output argument is the normal case and must work.
		got, err := resolveFileOutput(pngSprite, pngSprite)
		if err != nil {
			t.Fatalf("omitted output should be allowed: %v", err)
		}
		if filepath.Ext(got) != ".svg" {
			t.Fatalf("omitted output should resolve to a .svg, got %q", got)
		}

		// An explicit output that is a different path to the same file (here a
		// symlink) must be refused rather than overwriting the source image.
		link := filepath.Join(dir, "same-link.png")
		if err := os.Symlink(pngSprite, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := resolveFileOutput(pngSprite, link); err == nil {
			t.Fatalf("explicit same input/output should fail")
		}
	})
}

func TestHandleFileRejectsOverwritingInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "sprite.png")
	writePNG(t, input, pattern(4, 4))
	before := readFile(t, input)

	if err := handleFile(input, input, testTransparent, lib.Options{}); err == nil {
		t.Fatalf("handleFile should refuse to overwrite its input")
	}
	if after := readFile(t, input); after != before {
		t.Fatalf("input file was modified")
	}
}

func TestHandleFileMerge(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "sprite.png")
	writePNG(t, input, pattern(8, 8))

	plainPath := filepath.Join(dir, "plain.svg")
	mergedPath := filepath.Join(dir, "merged.svg")
	if err := handleFile(input, plainPath, testTransparent, lib.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := handleFile(input, mergedPath, testTransparent, lib.Options{Merge: true}); err != nil {
		t.Fatal(err)
	}

	plain := readFile(t, plainPath)
	merged := readFile(t, mergedPath)
	if !strings.Contains(plain, "stroke=") || strings.Contains(plain, "fill=") {
		t.Fatalf("default output should use stroked paths")
	}
	if !strings.Contains(merged, "fill=") || strings.Contains(merged, "stroke=") {
		t.Fatalf("merged output should use filled paths")
	}
	if strings.Count(merged, "M") >= strings.Count(plain, "M") {
		t.Fatalf("merge should reduce geometry commands: plain %d, merged %d",
			strings.Count(plain, "M"), strings.Count(merged, "M"))
	}
}

func TestHandleDirDetectsOutputConflict(t *testing.T) {
	input := t.TempDir()
	output := t.TempDir()
	writePNG(t, filepath.Join(input, "sprite.png"), pattern(2, 2))
	writePNG(t, filepath.Join(input, "sprite.jpg"), pattern(2, 2))

	err := handleDir(input, output, testTransparent, lib.Options{})
	if err == nil {
		t.Fatalf("expected a conflict error for sprite.png/sprite.jpg")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error should mention the conflict: %v", err)
	}
	if files := listFiles(t, output); len(files) != 0 {
		t.Fatalf("no output should be written on conflict, got %v", files)
	}
}

func TestHandleDirRejectsFileOutput(t *testing.T) {
	input := t.TempDir()
	writePNG(t, filepath.Join(input, "sprite.png"), pattern(2, 2))
	output := filepath.Join(t.TempDir(), "out.svg")
	if err := os.WriteFile(output, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := handleDir(input, output, testTransparent, lib.Options{}); err == nil {
		t.Fatalf("a file output must be rejected in batch mode")
	}
}

func TestHandleDirRejectsOverwritingInputSVG(t *testing.T) {
	// Converting a folder in place would make sprite.png overwrite sprite.svg,
	// which is also an input of the same batch.
	input := t.TempDir()
	writePNG(t, filepath.Join(input, "sprite.png"), pattern(2, 2))
	if err := os.WriteFile(filepath.Join(input, "sprite.svg"), []byte("<svg/>"), 0644); err != nil {
		t.Fatal(err)
	}

	err := handleDir(input, input, testTransparent, lib.Options{})
	if err == nil {
		t.Fatalf("expected a conflict error for in-place conversion")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error should mention the conflict: %v", err)
	}
	if got := readFile(t, filepath.Join(input, "sprite.svg")); got != "<svg/>" {
		t.Fatalf("existing svg was overwritten: %q", got)
	}
}

func TestHandleDirParallelIsReproducible(t *testing.T) {
	input := t.TempDir()
	for i := 0; i < 12; i++ {
		name := filepath.Join(input, "sprite"+string(rune('a'+i))+".png")
		writePNG(t, name, pattern(10+i, 9+i))
	}
	// Subdirectories must be skipped.
	if err := os.MkdirAll(filepath.Join(input, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, filepath.Join(input, "nested", "ignored.png"), pattern(2, 2))
	// A non-image file is reported and skipped, but must not abort the batch.
	if err := os.WriteFile(filepath.Join(input, "notes.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	run := func(parallelMode bool, options lib.Options) map[string]string {
		output := t.TempDir()
		previous := parallel
		parallel = parallelMode
		defer func() { parallel = previous }()
		if err := handleDir(input, output, testTransparent, options); err != nil {
			t.Fatalf("handleDir: %v", err)
		}
		files := listFiles(t, output)
		result := make(map[string]string, len(files))
		for _, name := range files {
			result[name] = readFile(t, filepath.Join(output, name))
		}
		return result
	}

	for _, options := range []lib.Options{{}, {Merge: true}} {
		want := run(false, options)
		if len(want) != 12 {
			t.Fatalf("expected 12 svg files, got %d: %v", len(want), keysOf(want))
		}
		for attempt := 0; attempt < 5; attempt++ {
			got := run(true, options)
			if len(got) != len(want) {
				t.Fatalf("parallel run wrote %d files, want %d", len(got), len(want))
			}
			for name, content := range want {
				if got[name] != content {
					t.Fatalf("parallel output for %s differs from serial output (Merge=%v)", name, options.Merge)
				}
			}
		}
	}
}

func keysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
