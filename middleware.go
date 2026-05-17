package wishsplash

import (
	"errors"
	"io"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/acarl005/stripansi"
	"github.com/charmbracelet/ssh"
	"github.com/creack/pty"
	"github.com/donovanhubbard/wish"
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

func scaleDown(opts Options, logger Logger) (Options, error) {
	logger.Debug("Scaling down text", "method", "scaleDown")
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

func renderText(opts Options, window ssh.Window, logger Logger) []string {
	logger.Debug("Starting renderText", "method", "renderText")
	font, err := ansifonts.LoadFont(opts.Font)
	if err != nil {
		logger.Error("Missing font '"+opts.Font+"' rendering text in plaintext", "method", "renderText")
		return []string{opts.Text}
	}

	var rendered []string
	fitsScreen := false

	logger.Info("window height="+strconv.Itoa(window.Height)+", width="+strconv.Itoa(window.Width), "method", "renderText")

	for fitsScreen == false {
		rendered = ansifonts.RenderTextWithOptions(opts.Text, font, opts.RenderOptions)
		textHeight, textWidth := getDimensions(rendered)

		logger.Info("Rendered text height="+strconv.Itoa(textHeight)+", width="+strconv.Itoa(textWidth), "method", "renderText")

		if textHeight > window.Height || textWidth > window.Width {
			logger.Warn("Rendered text is too big. Scaling down", "method", "renderText")
			opts, err = scaleDown(opts, logger)
			if err != nil {
				logger.Error("The rendered text is too big for the smallest scaling factor. Displaying plaintext", "method", "renderText")
				rendered = []string{opts.Text}
				fitsScreen = true
			}
		} else {
			fitsScreen = true
		}
	}

	logger.Debug("Exiting renderText", "method", "renderText")
	return rendered
}

func renderSplashScreen(sess ssh.Session, opts Options, window ssh.Window, logger Logger) {
	logger.Debug("Starting renderSplashScreen", "method", "renderSplashScreen")
	lines := make([]string, 0)
	rendered := renderText(opts, window, logger)

	height, width := getDimensions(rendered)

	logger.Info("Text height='"+strconv.Itoa(height)+"' width='"+strconv.Itoa(width)+"'", "method", "renderSplashScreen")
	logger.Info("Window height='"+strconv.Itoa(window.Height)+"' width='"+strconv.Itoa(window.Width)+"'", "method", "renderSplashScreen")

	paddingWidth := (window.Width - width) / 2
	paddingHeight := (window.Height - height) / 2

	logger.Info("paddingWidth='"+strconv.Itoa(paddingWidth)+"' paddingHeight='"+strconv.Itoa(paddingHeight)+"'", "method", "renderSplashScreen")

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

	logger.Debug("Exiting renderSplashScreen", "method", "renderSplashScreen")
}

func validateOptions(opts Options, logger Logger) error {
	logger.Debug("Starting validateOptions", "method", "validateOptions")
	var errorList error
	if opts.Font == "" {
		logger.Error("Options field Font cannot be empty", "method", "validateOptions")
		err := errors.New("Options field Font cannot be empty")
		errorList = errors.Join(errorList, err)
	}
	if opts.Text == "" {
		logger.Error("Options field Text cannot be empty", "method", "validateOptions")
		err := errors.New("Options field Text cannot be empty")
		errorList = errors.Join(errorList, err)
	}
	if opts.Delay == 0 {
		logger.Error("Options field Delay cannot be zero", "method", "validateOptions")
		err := errors.New("Options field Delay cannot be zero")
		errorList = errors.Join(errorList, err)
	}
	logger.Debug("Exiting validateOptions", "method", "validateOptions")
	return errorList
}

func WithOptions(opts Options) wish.Middleware {
	return WithLogger(opts, noopLogger{})
}

func WithLogger(opts Options, logger Logger) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sess ssh.Session) {

			logger.Info("Starting wishsplash middleware", "method", "withLogger")

			// Used to tell go routine this is done
			splashTerminated := false

			ptyReq, winCh, ok := sess.Pty()
			if !ok {
				logger.Error("Failed to get PTY", "method", "WithLogger")
				next(sess)
				return
			}

			err := validateOptions(opts, logger)
			if err != nil {
				logger.Error("Mandatory opts are not set properly.", "method", "WithLogger", "error", err)
				next(sess)
				return
			}

			renderSplashScreen(sess, opts, ptyReq.Window, logger)

			// re-render the screen if the winow resizes
			go func() {
				for win := range winCh {
					if splashTerminated == true {
						logger.Info("Terminating go routine to resize windows for splash screen", "method", "WithLogger")
						return
					} else {
						logger.Info("Window resized", "method", "WithLogger")
						renderSplashScreen(sess, opts, win, logger)
					}
				}
			}()

			cmdString := []string{"sleep", strconv.Itoa(opts.Delay)}

			cmd := exec.Command(cmdString[0], cmdString[1:]...)

			cmd.Env = append(cmd.Env,
				"TERM="+ptyReq.Term,
				"LANG=en_US.UTF-8",
			)

			ptmx, err := pty.Start(cmd)
			if err != nil {
				logger.Error("Failed to start sleep command", "method", "WithLogger", "error", err)
				next(sess)
				return
			}
			defer ptmx.Close()

			io.Copy(sess, ptmx)
			_ = cmd.Wait()

			sess.Write([]byte(RESTORE_CURSOR))
			sess.Write([]byte(SWITCH_TO_MAIN_BUFFER))
			splashTerminated = true
			logger.Info("Completed wishsplash middleware", "method", "WithLogger")
			next(sess)
		}
	}
}
