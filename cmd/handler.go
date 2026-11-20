package cmd

import (
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/devkanro/pvg/lib"
)

func handleDir(input string, output string, transparent color.Color, options lib.Options) error {
	files, err := ioutil.ReadDir(input)
	if err != nil {
		return err
	}

	stat, statErr := os.Stat(output)
	if statErr == nil && !stat.IsDir() {
		return fmt.Errorf("output '%s' must be a folder in batch mode", output)
	}
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}

	// Work out every input/output pair up front so output conflicts are
	// reported before anything is written. ioutil.ReadDir sorts by file name,
	// and the conflict check below does not depend on which worker runs first,
	// so batch output is reproducible even in parallel mode.
	pairs, err := buildFilePairs(input, output, files)
	if err != nil {
		return err
	}

	if statErr != nil {
		if err = os.MkdirAll(output, 0755); err != nil {
			return err
		}
	}

	if parallel {
		var waitGroup sync.WaitGroup
		cpus := runtime.NumCPU()
		if cpus > len(pairs) {
			cpus = len(pairs)
		}
		if cpus < 1 {
			cpus = 1
		}
		waitGroup.Add(cpus)
		for i := 0; i < cpus; i++ {
			go func(group int) {
				handleFolderGroup(pairs, cpus, group, transparent, options)
				waitGroup.Done()
			}(i)
		}
		waitGroup.Wait()
	} else {
		handleFolderGroup(pairs, 1, 0, transparent, options)
	}

	return nil
}

type filePair struct {
	input  string
	output string
}

// buildFilePairs maps every convertible file in files to its output path.
// Files that are not decodable images are still visited and reported by
// handleFile, exactly like before. Two situations would silently lose output,
// so they are rejected explicitly instead of depending on scheduling order:
//
//   - two inputs whose base names collide (a.png and a.jpg both map to a.svg);
//   - an output that is also an input of the same batch (only possible when
//     converting a folder in place, e.g. a.png alongside a.svg).
func buildFilePairs(input string, output string, files []os.FileInfo) ([]filePair, error) {
	pairs := make([]filePair, 0, len(files))
	seen := make(map[string]string, len(files))
	inputs := make(map[string]string, len(files))

	inputRoot, err := filepath.Abs(input)
	if err != nil {
		return nil, err
	}
	outputRoot, err := filepath.Abs(output)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		inputs[filepath.Join(inputRoot, file.Name())] = file.Name()
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()
		basename := strings.TrimSuffix(name, filepath.Ext(name))
		target := filepath.Join(outputRoot, basename+".svg")

		if previous, ok := seen[target]; ok {
			return nil, fmt.Errorf("output conflict: '%s' and '%s' both map to '%s'", previous, name, target)
		}
		if source, ok := inputs[target]; ok {
			return nil, fmt.Errorf("output conflict: '%s' would overwrite input file '%s'", name, source)
		}
		seen[target] = name
		pairs = append(pairs, filePair{
			input:  filepath.Join(input, name),
			output: filepath.Join(output, basename+".svg"),
		})
	}

	return pairs, nil
}

func handleFolderGroup(pairs []filePair, groupCount int, group int, transparent color.Color, options lib.Options) {
	for i, pair := range pairs {
		if groupCount > 1 && i%groupCount != group {
			continue
		}
		if err := handleFile(pair.input, pair.output, transparent, options); err != nil {
			fmt.Printf("Skip convert file '%s' due to error: %s\n", filepath.Base(pair.input), err)
		}
	}
}

func handleFile(input string, output string, transparent color.Color, options lib.Options) (err error) {
	inputPath, err := filepath.Abs(input)
	if err != nil {
		return err
	}
	outputPath, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	if samePath(inputPath, outputPath) {
		return fmt.Errorf("input and output are the same file '%s'", inputPath)
	}

	file, err := os.Open(input)
	if err != nil {
		return err
	}
	defer file.Close()

	img, format, err := image.Decode(file)
	if err != nil {
		return fmt.Errorf("Open file failed: %s ", err)
	}
	fmt.Printf("%s>>svg: '%s'.\n", format, input)

	if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}

	outputFile, err := os.Create(output)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := outputFile.Close(); err == nil {
			err = closeErr
		}
	}()

	_, err = outputFile.WriteString(lib.Convert(img, transparent, options))
	return err
}

func samePath(a, b string) bool {
	if a == b {
		return true
	}
	aInfo, aErr := os.Stat(a)
	bInfo, bErr := os.Stat(b)
	if aErr == nil && bErr == nil {
		return os.SameFile(aInfo, bInfo)
	}
	return false
}

func parseHexColor(s string) (c color.RGBA, err error) {
	c.A = 0xff
	switch len(s) {
	case 9:
		_, err = fmt.Sscanf(s, "#%02x%02x%02x%02x", &c.A, &c.R, &c.G, &c.B)
	case 7:
		_, err = fmt.Sscanf(s, "#%02x%02x%02x", &c.R, &c.G, &c.B)
	case 4:
		_, err = fmt.Sscanf(s, "#%1x%1x%1x", &c.R, &c.G, &c.B)
		c.R *= 17
		c.G *= 17
		c.B *= 17
	default:
		err = fmt.Errorf("invalid length, must be 4, 7 or 9")
	}
	return
}
