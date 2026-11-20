package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/devkanro/pvg/lib"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:     "pvg",
	Short:   "pvg is tool for convert pixel art to SVG.",
	Long:    `Convert pixel art bitmap to SVG without tracing, just keep all pixel exist.`,
	Version: "v1.0",
	Args:    cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		input := args[0]
		output := input
		if len(args) == 2 {
			output = args[1]
		}

		transparent, err := parseHexColor(transparentColor)
		if err != nil {
			return fmt.Errorf("Wrong transparent color '%s', it must be a valid hex color value ", transparentColor)
		}

		stat, err := os.Stat(input)
		if err != nil {
			return err
		}

		options := lib.Options{Merge: merge}
		if stat.IsDir() {
			err = handleDir(input, output, transparent, options)
		} else {
			output, err = resolveFileOutput(input, output)
			if err != nil {
				return err
			}
			err = handleFile(input, output, transparent, options)
		}
		return err
	},
}

// resolveFileOutput turns the second argument (or the input path, when it was
// omitted) into a concrete .svg file path, matching the historical behaviour:
//
//   - no output argument: write <input without extension>.svg next to the input
//   - output is an existing directory: write <dir>/<basename>.svg inside it
//   - output is an existing file: write to that file path as given
//   - output does not exist and has an .svg extension: write to that path
//   - output does not exist and has any other extension: treat it as a folder
//     and write <output>/<basename>.svg
//
// If the resolved path is the input file itself, an error is returned rather
// than overwriting the source image.
func resolveFileOutput(input string, output string) (string, error) {
	basename := filepath.Base(input)
	basename = basename[:len(basename)-len(filepath.Ext(basename))]

	if input == output {
		output = filepath.Join(filepath.Dir(input), basename+".svg")
	}

	stat, statErr := os.Stat(output)
	switch {
	case statErr != nil && !os.IsNotExist(statErr):
		return "", statErr
	case statErr == nil && stat.IsDir():
		output = filepath.Join(output, basename+".svg")
	case statErr != nil && filepath.Ext(output) != ".svg":
		// A non-existing path without an .svg extension is a folder.
		output = filepath.Join(output, basename+".svg")
	}

	absoluteInput, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	absoluteOutput, err := filepath.Abs(output)
	if err != nil {
		return "", err
	}
	if samePath(absoluteInput, absoluteOutput) {
		return "", fmt.Errorf("input and output are the same file '%s'", absoluteOutput)
	}

	return output, nil
}

var transparentColor string
var parallel bool
var merge bool

func init() {
	rootCmd.Flags().StringVarP(&transparentColor, "transparent", "t", "#FFFF00FF",
		"transparent color for converting, default as purple.")
	rootCmd.Flags().BoolVarP(&parallel, "parallel", "p", true,
		"parallel process in folder mode.")
	rootCmd.Flags().BoolVarP(&merge, "merge", "m", false,
		"merge horizontal same-color pixels (and equal-span adjacent rows) into fewer SVG nodes.")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
