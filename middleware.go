package wishsplash

import (
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/wish/v2"
	"github.com/acarl005/stripansi"
	"github.com/charmbracelet/ssh"
	"github.com/creack/pty"
	"github.com/superstarryeyes/bit/ansifonts"
)

const (
	SWITCH_TO_ALT_BUFFER  = "\x1b[?1049h"
	SWITCH_TO_MAIN_BUFFER = "\x1b[?1049l"
	HIDE_CURSOR           = "\x1b[?25l"
	RESTORE_CURSOR        = "\x1b[?25h"
)

type Options struct {
	Text          string
	Font          string
	RenderOptions ansifonts.RenderOptions
	Delay         int
}

func countPrintableCharacters(line string) int {
	//remove ANSI escape codes
	cleanLine := stripansi.Strip(line)
	return utf8.RuneCountInString(cleanLine)
}

func getDimensions(rendered []string) (int, int) {
	height := len(rendered)
	width := 0

	for _, line := range rendered {
		lineWidth := countPrintableCharacters(line)

		if lineWidth > width {
			width = lineWidth
		}
	}
	return height, width
}

func scaleDown(opts Options) (Options, error) {
	supportedScaleFactors := []float64{0.5, 1.0, 2.0, 4.0}

	index := slices.Index(supportedScaleFactors, opts.RenderOptions.ScaleFactor)

	// Invalid scale factor. Defaults to 1.0
	if index < 0 {
		index = 1
	} else if index == 0 { // can't go smaller
		return opts, errors.New("Can't scale down farther")
	} else {
		index = index - 1
	}

	opts.RenderOptions.ScaleFactor = supportedScaleFactors[index]

	return opts, nil
}

func renderText(opts Options, window ssh.Window) []string {
	font, err := ansifonts.LoadFont(opts.Font)
	if err != nil {
		return []string{opts.Text}
	}

	var rendered []string
	fitsScreen := false

	for fitsScreen == false {
		rendered = ansifonts.RenderTextWithOptions(opts.Text, font, opts.RenderOptions)
		textHeight, textWidth := getDimensions(rendered)

		if textHeight > window.Height || textWidth > window.Width {
			opts, err = scaleDown(opts)
			if err != nil {
				rendered = []string{opts.Text}
				fitsScreen = true
			}
		} else {
			fitsScreen = true
		}
	}
	return rendered
}

func renderSplashScreen(sess ssh.Session, opts Options, window ssh.Window) {
	lines := make([]string, 0)
	rendered := renderText(opts, window)

	height, width := getDimensions(rendered)

	paddingWidth := (window.Width - width) / 2
	paddingHeight := (window.Height - height) / 2

	lines = append(lines, SWITCH_TO_ALT_BUFFER)
	lines = append(lines, HIDE_CURSOR)

	for range paddingHeight {
		lines = append(lines, "\r\n")
	}
	lines = append(lines, "\r\n")

	for _, line := range rendered {
		var sb strings.Builder
		for range paddingWidth {
			sb.WriteString(" ")
		}
		sb.WriteString(line + "\r\n")
		lines = append(lines, sb.String())
	}

	for range paddingHeight {
		lines = append(lines, "\r\n")
	}

	for _, line := range lines {
		sess.Write([]byte(line))
	}
}

func WithOptions(opts Options) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sess ssh.Session) {

			ptyReq, winCh, ok := sess.Pty()
			if !ok {
				next(sess)
				return
			}

			renderSplashScreen(sess, opts, ptyReq.Window)

			// re-render the screen if the winow resizes
			go func() {
				for win := range winCh {
					renderSplashScreen(sess, opts, win)
				}
			}()

			cmdString := []string{"sleep", strconv.Itoa(opts.Delay)}
			slog.Debug("executing", "command", strings.Join(cmdString, " "))

			cmd := exec.Command(cmdString[0], cmdString[1:]...)

			cmd.Env = append(cmd.Env,
				"TERM="+ptyReq.Term,
				"LANG=en_US.UTF-8",
			)

			ptmx, err := pty.Start(cmd)
			if err != nil {
				next(sess)
				return
			}
			defer ptmx.Close()

			io.Copy(sess, ptmx)
			_ = cmd.Wait()

			sess.Write([]byte(RESTORE_CURSOR))
			sess.Write([]byte(SWITCH_TO_MAIN_BUFFER))

			next(sess)
		}
	}
}
