package auth

import "github.com/emersion/go-sasl"

// xoauth2Client implements Google's XOAUTH2 SASL mechanism
// (https://developers.google.com/gmail/imap/xoauth2-protocol), which
// go-sasl doesn't ship: Gmail's IMAP/SMTP servers accept AUTHENTICATE/AUTH
// XOAUTH2 directly regardless of whether it's advertised in their
// capability list, so this needs no server negotiation beyond sending the
// mechanism name go-imap/go-smtp already handle generically via
// sasl.Client.
type xoauth2Client struct {
	username, token string
}

// newXOAuth2Client returns a sasl.Client for Google's XOAUTH2 mechanism,
// authenticating as username with an OAuth2 access token.
func newXOAuth2Client(username, token string) sasl.Client {
	return &xoauth2Client{username: username, token: token}
}

func (c *xoauth2Client) Start() (mech string, ir []byte, err error) {
	ir = []byte("user=" + c.username + "\x01auth=Bearer " + c.token + "\x01\x01")
	return "XOAUTH2", ir, nil
}

// Next handles the one-shot error challenge Google sends when the initial
// response is rejected: a base64-decoded JSON error blob. RFC 7628-style
// mechanisms (which XOAUTH2 predates but mirrors) expect the client to
// reply with an empty message to close out the exchange; the server then
// fails the command with the real error.
func (c *xoauth2Client) Next(challenge []byte) ([]byte, error) {
	return []byte{}, nil
}
