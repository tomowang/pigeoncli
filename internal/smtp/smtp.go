package smtp

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/emersion/go-smtp"

	"github.com/tomowang/pigeoncli/internal/auth"
	"github.com/tomowang/pigeoncli/internal/config"
)

// DialOptions describes how to connect to an SMTP server.
type DialOptions struct {
	Host string
	Port int
	TLS  config.TLSMode
}

func (o DialOptions) addr() string {
	return net.JoinHostPort(o.Host, strconv.Itoa(o.Port))
}

func dial(o DialOptions) (*smtp.Client, error) {
	switch o.TLS {
	case config.TLSModeTLS:
		return smtp.DialTLS(o.addr(), nil)
	case config.TLSModeSTARTTLS:
		return smtp.DialStartTLS(o.addr(), nil)
	default:
		return smtp.Dial(o.addr())
	}
}

// TestLogin dials the SMTP server, authenticates using provider, and
// disconnects. It's used by `pigeon account test` to verify account
// settings.
func TestLogin(ctx context.Context, opts DialOptions, provider auth.Provider) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- testLogin(ctx, opts, provider)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func testLogin(ctx context.Context, opts DialOptions, provider auth.Provider) error {
	c, err := dial(opts)
	if err != nil {
		return fmt.Errorf("dial %s: %w", opts.addr(), err)
	}
	defer c.Close()

	saslClient, err := provider.SMTPSASLClient(ctx)
	if err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	if err := c.Auth(saslClient); err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}
	if err := c.Quit(); err != nil {
		return fmt.Errorf("quit: %w", err)
	}
	return nil
}
