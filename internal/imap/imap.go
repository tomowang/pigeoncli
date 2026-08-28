package imap

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/tomowang/pigeoncli/internal/auth"
	"github.com/tomowang/pigeoncli/internal/config"
)

// DialOptions describes how to connect to an IMAP server.
type DialOptions struct {
	Host string
	Port int
	TLS  config.TLSMode
}

func (o DialOptions) addr() string {
	return net.JoinHostPort(o.Host, strconv.Itoa(o.Port))
}

func dial(o DialOptions, opts *imapclient.Options) (*imapclient.Client, error) {
	switch o.TLS {
	case config.TLSModeTLS:
		return imapclient.DialTLS(o.addr(), opts)
	case config.TLSModeSTARTTLS:
		return imapclient.DialStartTLS(o.addr(), opts)
	default:
		return imapclient.DialInsecure(o.addr(), opts)
	}
}

// TestLogin dials the IMAP server, authenticates using provider, and logs
// out. It's used by `pigeon account test` to verify account settings.
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
	c, err := dial(opts, nil)
	if err != nil {
		return fmt.Errorf("dial %s: %w", opts.addr(), err)
	}
	defer func() { _ = c.Close() }()

	if err := c.WaitGreeting(); err != nil {
		return fmt.Errorf("greeting: %w", err)
	}

	saslClient, err := provider.IMAPSASLClient(ctx)
	if err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	if err := c.Authenticate(saslClient); err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}
	if err := c.Logout().Wait(); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}
