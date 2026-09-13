package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/emersion/go-sasl"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// googleOAuthClientID and googleOAuthClientSecret identify pigeon to
// Google's OAuth2 endpoint as a "Desktop app" OAuth client. Per Google's
// own guidance for installed applications
// (https://developers.google.com/identity/protocols/oauth2/native-app),
// such a client secret isn't actually confidential — it ships inside a
// public, unauthenticated binary the same way Thunderbird's and rclone's
// do — so baking it in means Gmail accounts work out of the box, without
// every user registering their own Google Cloud project.
//
// PIGEON_GOOGLE_OAUTH_CLIENT_ID / _SECRET override these for local
// testing against a different Google Cloud project without a rebuild.
const (
	googleOAuthClientID     = "447315878179-pa8ncso0p5ltlm8h6jef19dnmip3jkil.apps.googleusercontent.com"
	googleOAuthClientSecret = "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"
)

// googleOAuthScope is the only OAuth scope that grants IMAP/SMTP access
// (i.e. XOAUTH2) to a Gmail account; the narrower gmail.googleapis.com
// scopes only cover the REST API, not the mail protocols pigeon speaks.
const googleOAuthScope = "https://mail.google.com/"

func googleOAuthConfig() *oauth2.Config {
	clientID := googleOAuthClientID
	if v := os.Getenv("PIGEON_GOOGLE_OAUTH_CLIENT_ID"); v != "" {
		clientID = v
	}
	clientSecret := googleOAuthClientSecret
	if v := os.Getenv("PIGEON_GOOGLE_OAUTH_CLIENT_SECRET"); v != "" {
		clientSecret = v
	}
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{googleOAuthScope},
	}
}

// GoogleProvider authenticates using an OAuth2 token stored in the OS
// keyring under AccountSlug, refreshing it via Google's token endpoint
// when expired. Obtain the initial token with LoginGoogle.
type GoogleProvider struct {
	Username    string
	AccountSlug string
}

func (p GoogleProvider) IMAPSASLClient(ctx context.Context) (sasl.Client, error) {
	return p.saslClient(ctx)
}

func (p GoogleProvider) SMTPSASLClient(ctx context.Context) (sasl.Client, error) {
	return p.saslClient(ctx)
}

func (p GoogleProvider) saslClient(ctx context.Context) (sasl.Client, error) {
	token, err := p.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	return newXOAuth2Client(p.Username, token), nil
}

// accessToken returns a valid access token for AccountSlug, transparently
// refreshing (and persisting back to the keyring) an expired one.
func (p GoogleProvider) accessToken(ctx context.Context) (string, error) {
	stored, err := GetOAuthToken(p.AccountSlug)
	if err != nil {
		return "", fmt.Errorf("credentials for %q: %w", p.AccountSlug, err)
	}
	fresh, err := googleOAuthConfig().TokenSource(ctx, stored).Token()
	if err != nil {
		return "", fmt.Errorf("refresh google token for %q: %w", p.AccountSlug, err)
	}
	if fresh.AccessToken != stored.AccessToken {
		if fresh.RefreshToken == "" {
			// Google's token endpoint omits refresh_token from a refresh
			// response; without carrying the original forward, the next
			// refresh would fail with an empty RefreshToken.
			fresh.RefreshToken = stored.RefreshToken
		}
		if err := SetOAuthToken(p.AccountSlug, fresh); err != nil {
			return "", err
		}
	}
	return fresh.AccessToken, nil
}

// googleLoginTimeout bounds how long LoginGoogle waits for the user to
// complete the consent screen before giving up.
const googleLoginTimeout = 5 * time.Minute

// LoginGoogle runs Google's OAuth2 "installed app" loopback flow: it
// starts a one-shot local HTTP server, builds a PKCE-protected consent URL
// and passes it to onURL (so the caller can display it and/or open a
// browser), then blocks until Google redirects back to the loopback
// server with an authorization code, ctx is canceled, or
// googleLoginTimeout elapses. On success it returns the exchanged token,
// which callers must persist themselves (e.g. via SetOAuthToken).
func LoginGoogle(ctx context.Context, onURL func(url string)) (*oauth2.Token, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start local oauth2 callback listener: %w", err)
	}
	defer func() { _ = listener.Close() }()

	state, err := randomURLSafeString(16)
	if err != nil {
		return nil, err
	}
	verifier, err := randomURLSafeString(32)
	if err != nil {
		return nil, err
	}
	challengeSum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeSum[:])

	cfg := googleOAuthConfig()
	cfg.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)
	authURL := cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"), // force a refresh_token on every login, not just the first
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)

	type result struct {
		code string
		err  error
	}
	resultCh := make(chan result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errParam := q.Get("error"); errParam != "" {
			http.Error(w, "Authorization failed. You can close this tab.", http.StatusOK)
			resultCh <- result{err: fmt.Errorf("google authorization denied: %s", errParam)}
			return
		}
		if q.Get("state") != state {
			http.Error(w, "Authorization failed: state mismatch.", http.StatusBadRequest)
			resultCh <- result{err: fmt.Errorf("oauth2 callback: state mismatch")}
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "Authorization failed: no code received.", http.StatusBadRequest)
			resultCh <- result{err: fmt.Errorf("oauth2 callback: missing code")}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>Signed in. You can close this tab and return to pigeon.</body></html>"))
		resultCh <- result{code: code}
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	onURL(authURL)
	openBrowser(authURL)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(googleLoginTimeout):
		return nil, fmt.Errorf("timed out waiting for google sign-in")
	case res := <-resultCh:
		if res.err != nil {
			return nil, res.err
		}
		token, err := cfg.Exchange(ctx, res.code, oauth2.SetAuthURLParam("code_verifier", verifier))
		if err != nil {
			return nil, fmt.Errorf("exchange google authorization code: %w", err)
		}
		return token, nil
	}
}

func randomURLSafeString(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// openBrowser best-effort opens url in the user's default browser. Failure
// is silent: LoginGoogle already hands the URL to its caller to display,
// so a headless environment or missing browser just means the user copies
// it manually.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
