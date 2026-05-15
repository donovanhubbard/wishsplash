package main

import (
	"errors"
	"net"

	"charm.land/log/v2"
	"charm.land/wish/v2"
	"charm.land/wish/v2/logging"
	"github.com/charmbracelet/ssh"
	"github.com/donovanhubbard/wishsplash"
	"github.com/superstarryeyes/bit/ansifonts"
)

const (
	host = "localhost"
	port = "23234"
)

func main() {

	// Setup your bit and splash screen options
	opts := wishsplash.Options{
		Font:  "8bitfortress",
		Text:  "Hello Dude",
		Delay: 25, // Number of seconds the splash screen will display
		RenderOptions: ansifonts.RenderOptions{
			CharSpacing:            3,
			WordSpacing:            2,
			LineSpacing:            2,
			TextColor:              "#F000FF",
			GradientColor:          "#00FF00",
			UseGradient:            false,
			GradientDirection:      ansifonts.LeftRight,
			Alignment:              ansifonts.RightAlign,
			ScaleFactor:            3.0,
			ShadowEnabled:          true,
			ShadowHorizontalOffset: 1,
			ShadowVerticalOffset:   1,
			ShadowStyle:            ansifonts.MediumShade,
		},
	}

	srv, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort(host, port)),
		wish.WithHostKeyPath(".ssh/id_ed25519"),

		wish.WithMiddleware(
			func(next ssh.Handler) ssh.Handler {
				return func(sess ssh.Session) {
					wish.Println(sess, "Hello, world!")
					next(sess)
				}
			},

			// Add your splash screen middleware. Middleware is executed in reverse
			// order so you will want to add it towards the end
			logging.Middleware(),
			wishsplash.WithOptions(opts),
		),
	)
	if err != nil {
		log.Error("Could not start server", "error", err)
	}

	log.Info("Starting SSH server", "host", host, "port", port)
	if err = srv.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		log.Error("Could not start server", "error", err)
	}
}
