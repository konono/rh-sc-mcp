package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Paths served by the HTTP transport. The probes sit outside the
// authenticated handler so a kubelet can reach them without a
// credential; they disclose nothing about Red Hat or the scope.
const (
	MCPPath    = "/mcp"
	HealthPath = "/healthz"
	ReadyPath  = "/readyz"
)

// Timeouts for the listener. WriteTimeout is deliberately absent: a
// streamed MCP response is long-lived by design, and a write deadline
// would sever it mid-answer.
const (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 120 * time.Second
	shutdownTimeout   = 20 * time.Second
)

// Handler builds the HTTP handler for the Streamable HTTP transport.
//
// Only the MCP endpoint is authenticated, and it always is. Exposing
// this server on a port hands the caller everything the offline token
// reaches within the configured scope, so there is no unauthenticated
// path to /mcp — the check here is deliberately duplicated with the one
// in config, because that one can be reached by a caller who built a
// Config by hand.
func (s *Server) Handler(logger *slog.Logger) (http.Handler, error) {
	token := s.cfg.HTTP.Token
	if token == "" {
		return nil, errors.New("refusing to serve MCP over HTTP without a bearer token")
	}
	// The same guard as config.validateHTTP. A Config assembled in code
	// rather than loaded from a file would otherwise slip past it.
	if token == s.cfg.OfflineToken {
		return nil, errors.New(
			"refusing to serve MCP over HTTP: the bearer token is the same string as the Red Hat offline token, " +
				"which would give every client direct access to the support API")
	}

	opts := &mcp.StreamableHTTPOptions{
		Stateless: s.cfg.HTTP.IsStateless(),
		Logger:    logger,
	}
	if !opts.Stateless && s.cfg.HTTP.SessionTimeoutSeconds > 0 {
		opts.SessionTimeout = time.Duration(s.cfg.HTTP.SessionTimeoutSeconds) * time.Second
	}
	// One Server instance serves every request. The scope is a property
	// of the process, not of the caller, so there is nothing per-request
	// to build — and nothing a caller can say to widen it.
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s.mcp }, opts)

	// Guards against a page in a browser driving this endpoint with
	// whatever ambient credentials the browser would attach, and against
	// a DNS-rebinding attack reaching a server bound to localhost.
	protection := http.NewCrossOriginProtection()
	for _, origin := range s.cfg.HTTP.TrustedOrigins {
		if err := protection.AddTrustedOrigin(origin); err != nil {
			return nil, fmt.Errorf("http.trustedOrigins: %w", err)
		}
	}

	authenticated := auth.RequireBearerToken(
		staticTokenVerifier(token),
		&auth.RequireBearerTokenOptions{
			// The shared token is not an OAuth access token and carries
			// no expiry of its own; its lifetime is however long the
			// operator leaves it in the secret.
			AllowMissingExpiration: true,
		},
	)(protection.Handler(mcpHandler))

	mux := http.NewServeMux()
	mux.Handle(MCPPath, authenticated)
	mux.HandleFunc(HealthPath, okHandler)
	mux.HandleFunc(ReadyPath, okHandler)
	return mux, nil
}

// okHandler answers the probes.
//
// It reports that this process is up and nothing else. In particular it
// does not call SSO or the support API: a probe that did would spend a
// token exchange on every poll and would flap whenever Red Hat had a
// blip, turning an upstream hiccup into a restart loop.
func okHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// staticTokenVerifier accepts exactly one shared token.
//
// The comparison is constant-time so a caller cannot recover the token a
// byte at a time by measuring how long the rejection takes.
func staticTokenVerifier(expected string) auth.TokenVerifier {
	want := []byte(expected)
	return func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		if subtle.ConstantTimeCompare([]byte(token), want) != 1 {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{Scopes: []string{}}, nil
	}
}

// ServeHTTP runs the HTTP transport until ctx is cancelled, then drains
// in-flight requests.
//
// The drain matters under an orchestrator: a pod is sent SIGTERM and
// removed from the Service at the same moment, so a request already in
// flight has to be allowed to finish instead of being cut off.
func (s *Server) ServeHTTP(ctx context.Context, logger *slog.Logger) error {
	handler, err := s.Handler(logger)
	if err != nil {
		return err
	}
	httpSrv := &http.Server{
		Addr:              s.cfg.HTTP.Addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}

	errc := make(chan error, 1)
	go func() {
		logger.Info("serving MCP over HTTP",
			"addr", s.cfg.HTTP.Addr, "path", MCPPath, "stateless", s.cfg.HTTP.IsStateless())
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
			return
		}
		errc <- nil
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}
