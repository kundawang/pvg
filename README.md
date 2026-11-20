# Description
pvg is tool for convert pixel art to SVG.

usage
```Shell
$ pvg --help
Convert pixel art bitmap to SVG without tracing, just keep all pixel exist.

Usage:
  pvg [flags]

Flags:
  -h, --help                 help for pvg
  -m, --merge                merge horizontal same-color pixels (and equal-span adjacent rows) into fewer SVG nodes.
  -p, --parallel             parallel process in folder mode. (default true)
  -t, --transparent string   transparent color for converting, default as purple. (default "#FFFF00FF")
  -v, --version              version for pvg
```

## Same-color merge mode

By default every horizontal run of one color becomes a `M x y h len` command on
a stroked `<path>`. Large pixel art can produce tens of thousands of those
commands, which makes SVG editors slow.

Pass `--merge` (`-m`) to emit the same pixels as merged rectangles instead:

- Each horizontal run is built exactly as in the default mode.
- Runs with the same span (same X and same width) on consecutive rows are
  stacked into one filled rectangle, so solid areas collapse quickly.
- The output still contains one `<path>` per color, but each rectangle is a
  single closed subpath. Colors are emitted in ascending RGB order, so the
  result is byte-for-byte reproducible, including parallel folder conversion.

```Shell
$ pvg sprite.png sprite.svg --merge
```

The rendered pixels are identical to the default mode. On a 512x512 8px
checkerboard the default output has 32,768 run commands; `--merge` renders the
same pixels with 4,096 rectangles.

## Rules and edge cases

- **Transparent color** (`-t`) and **alpha** are unchanged: a pixel is skipped
  when its premultiplied RGB key equals the transparent color, and
  semi-transparent pixels keep the same key as before. Merge mode applies the
  same rule; it only changes how remaining pixels are drawn.
- **Layer order** is deterministic: colors are sorted by RGB value and geometry
  follows row-major scan order.
- **Non-zero bounds** (`img.Bounds().Min != (0,0)`, e.g. `SubImage`) are
  normalized to the image size; output matches the zero-based equivalent.
- **Paletted images** are handled through the standard `image.Image`
  interface, with the same palette-entry transparency rule.
- **Very large images** use O(number of runs) memory instead of O(pixels).
- **Empty images** (nil, zero width or zero height) produce an empty `<svg>`
  with the correct `viewBox` instead of failing.
- **Output conflicts** in folder mode are reported before anything is written:
  two inputs mapping to the same `.svg` (e.g. `a.png` and `a.jpg`), or an
  output that would overwrite an input file during in-place conversion.
  Conversion output can never overwrite its own source image.

