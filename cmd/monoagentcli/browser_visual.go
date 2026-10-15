package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/visualdiff"
)

// browser diff · sweep — visual checks: compare two screenshots, and shoot
// the tab at several widths and color schemes (internal/visualdiff). Plan C3.

func init() {
	browserSubcommands = append(browserSubcommands, newBrowserDiffCmd, newBrowserSweepCmd)
}

// defaultDiffThreshold tolerates anti-aliasing and JPEG noise.
const defaultDiffThreshold = 16

func newBrowserDiffCmd(cfg *globalConfig) *cobra.Command {
	var out string
	var threshold int
	cmd := &cobra.Command{
		Use:   "diff <a.png> <b.png>",
		Short: "Compare two screenshots pixel by pixel and save an image of the differences",
		Long: "Compare two PNG or JPEG images and print {changed,ratio,path,total,width,height,\n" +
			"sizeMismatch}: changed is the number of pixels whose red, green or blue differs by more\n" +
			"than --threshold (0–255) once both are put on white, ratio is changed/total, and path is\n" +
			"the diff image (A faded to gray, changes in red, the area only one image covers in\n" +
			"magenta). Images of different sizes are compared over the larger one. Without --out the\n" +
			"diff goes to ./.monoagent/screenshots/<yyyymmdd-hhmmss>-<n>.png. Needs no browser.",
		Example: `  monoagentcli --json browser diff .monoagent/context/q1/element.png after.png
  monoagentcli browser diff before.png after.png --threshold 0 --out diff.png`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if threshold < 0 || threshold > visualdiff.MaxThreshold {
				return errInvalidInput("--threshold is 0 to %d, not %d", visualdiff.MaxThreshold, threshold)
			}
			res, err := visualdiff.CompareFiles(args[0], args[1], visualdiff.Options{Threshold: threshold})
			if err != nil {
				if os.IsNotExist(err) {
					return errNotFound("%v", err)
				}
				return errInvalidInput("%v", err)
			}
			var buf bytes.Buffer
			if err := png.Encode(&buf, res.Diff); err != nil {
				return err
			}
			path, err := browserctl.WriteScreenshot("", out, &browserctl.Shot{Data: buf.Bytes(), Mime: "image/png"})
			if err != nil {
				return err
			}
			return browserOut(cmd, cfg, map[string]any{
				"changed": res.Changed, "ratio": res.Ratio, "path": path, "total": res.Total,
				"width": res.Width, "height": res.Height, "sizeMismatch": res.SizeMismatch,
				"sizeA": res.SizeA, "sizeB": res.SizeB,
			}, func() {
				w := cmd.OutOrStdout()
				fmt.Fprintf(w, "%d of %d pixels changed (%.2f%%); diff image: %s\n", res.Changed, res.Total, res.Ratio*100, path)
				if res.SizeMismatch {
					fmt.Fprintf(w, "sizes differ: %dx%d vs %dx%d\n", res.SizeA.Width, res.SizeA.Height, res.SizeB.Width, res.SizeB.Height)
				}
			})
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "Diff image to write (default: under ./.monoagent/screenshots)")
	cmd.Flags().IntVar(&threshold, "threshold", defaultDiffThreshold, "Per-channel tolerance, 0 (exact) to 255")
	return cmd
}

// sweepShot is one screenshot of a sweep.
type sweepShot struct {
	Width       int    `json:"width"`
	Media       string `json:"media"`
	Path        string `json:"path"`
	ImageWidth  int    `json:"imageWidth"`
	ImageHeight int    `json:"imageHeight"`
}

func newBrowserSweepCmd(cfg *globalConfig) *cobra.Command {
	var widthsFlag, mediaFlag, out, element string
	var height int
	var viewport bool
	cmd := &cobra.Command{
		Use:   "sweep",
		Short: "Screenshot the tab at several widths, light and dark, to check a responsive layout",
		Long: "Set the tab's viewport to each --widths width (--height tall) and color scheme in\n" +
			"--media, save a full-page screenshot of each (--viewport: only the viewport; --element:\n" +
			"only that element) as <width>-<media>.png, and reset the tab's viewport and color scheme\n" +
			"afterwards. Without --out the shots go to a new ./.monoagent/screenshots/sweep-<stamp>-<n>/\n" +
			"folder. Prints {dir,shots:[{width,media,path,imageWidth,imageHeight}]}.",
		Example: `  monoagentcli --json browser sweep
  monoagentcli --json browser sweep --widths 320,1024 --media dark --element "main"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			widths, err := sweepWidths(widthsFlag)
			if err != nil {
				return errInvalidInput("%v", err)
			}
			media, err := sweepMedia(mediaFlag)
			if err != nil {
				return errInvalidInput("%v", err)
			}
			if height <= 0 {
				return errInvalidInput("--height must be positive")
			}
			if viewport && element != "" {
				return errInvalidInput("use --viewport or --element, not both")
			}
			opts := browserctl.ScreenshotOptions{Mode: "full"}
			switch {
			case viewport:
				opts.Mode = "viewport"
			case element != "":
				opts.Mode, opts.Selector = "element", element
			}
			r, err := openBrowser(cmd, cfg, false)
			if err != nil {
				return err
			}
			defer r.Close()
			dir, err := sweepDir(out)
			if err != nil {
				return err
			}
			shots := []sweepShot{}
			err = r.act(func() (err error) {
				defer func() {
					if rerr := r.s.Emulate(browserctl.Emulation{Reset: true}); err == nil && rerr != nil {
						err = fmt.Errorf("reset the viewport: %w", rerr)
					}
				}()
				for _, w := range widths {
					for _, m := range media {
						if err := r.s.Emulate(browserctl.Emulation{Width: w, Height: height, ColorScheme: m}); err != nil {
							return err
						}
						browserctl.Settle() // let the page re-lay out and repaint
						shot, err := r.s.Screenshot(opts)
						if err != nil {
							return fmt.Errorf("%dpx %s: %w", w, m, err)
						}
						path, err := browserctl.WriteScreenshot("", filepath.Join(dir, fmt.Sprintf("%d-%s%s", w, m, shot.Ext())), shot)
						if err != nil {
							return err
						}
						shots = append(shots, sweepShot{Width: w, Media: m, Path: path, ImageWidth: shot.Width, ImageHeight: shot.Height})
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			return r.out(map[string]any{"dir": dir, "shots": shots}, func() {
				for _, s := range shots {
					fmt.Fprintf(cmd.OutOrStdout(), "%5dpx %-5s %s\n", s.Width, s.Media, s.Path)
				}
			})
		},
	}
	cmd.Flags().StringVar(&widthsFlag, "widths", "375,768,1440", "Viewport widths in CSS pixels, comma-separated")
	cmd.Flags().StringVar(&mediaFlag, "media", "light,dark", "Color schemes, comma-separated: light, dark")
	cmd.Flags().IntVar(&height, "height", 900, "Viewport height in CSS pixels")
	cmd.Flags().BoolVar(&viewport, "viewport", false, "Shoot only the viewport, not the full page")
	cmd.Flags().StringVar(&element, "element", "", "Shoot only the element this CSS selector matches")
	cmd.Flags().StringVar(&out, "out", "", "Folder for the shots (default: a new one under ./.monoagent/screenshots)")
	return cmd
}

// maxSweepWidth is wider than any screen a layout targets.
const maxSweepWidth = 7680

func sweepWidths(s string) ([]int, error) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(part), "px"))
		if part == "" {
			continue
		}
		w, err := strconv.Atoi(part)
		if err != nil || w < 1 || w > maxSweepWidth {
			return nil, fmt.Errorf("--widths takes widths from 1 to %d, not %q", maxSweepWidth, part)
		}
		out = append(out, w)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--widths is empty")
	}
	return out, nil
}

func sweepMedia(s string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		m, err := browserctl.MediaFlag(part)
		if err != nil {
			return nil, fmt.Errorf("--media takes light and dark, not %q", strings.TrimSpace(part))
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--media is empty")
	}
	return out, nil
}

func sweepDir(out string) (string, error) {
	if out != "" {
		abs, err := filepath.Abs(out)
		if err != nil {
			return "", err
		}
		return abs, os.MkdirAll(abs, 0o755)
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return newBrowserStampedDir(browserctl.ScreenshotsDir(wd), "sweep-")
}
