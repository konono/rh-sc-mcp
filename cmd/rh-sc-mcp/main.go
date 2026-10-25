// Command rh-sc-mcp is an MCP server for the Red Hat Support Case
// Management API, with operator-side scope control: the configuration
// decides which resources an AI client can reach, and tools outside that
// scope are never exposed.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/konono/rh-sc-mcp/internal/config"
	"github.com/konono/rh-sc-mcp/internal/server"
	"github.com/konono/rh-sc-mcp/internal/update"
)

func main() {
	var (
		cfgPath     = flag.String("config", os.Getenv("RH_SC_MCP_CONFIG"), "path to the configuration YAML (default: ~/.config/rh-sc-mcp/config.yaml, created on first use; also $RH_SC_MCP_CONFIG)")
		printScope  = flag.Bool("print-scope", false, "print the resolved scope and the tools it exposes, then exit")
		checkAuth   = flag.Bool("check-auth", false, "exchange the offline token and probe the API, then exit; prints no credential")
		showVersion = flag.Bool("version", false, "print version and exit")
		httpAddr    = flag.String("http", "", "serve MCP over Streamable HTTP on this address (e.g. :8080) instead of stdio; also `http.addr` or $RH_SC_MCP_HTTP_ADDR")
	)
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: rh-sc-mcp [flags] | rh-sc-mcp update")
		fmt.Fprintln(os.Stderr, "  update  Install the latest verified GitHub Release over this executable")
		flag.PrintDefaults()
	}
	flag.Parse()
	if args := flag.Args(); len(args) > 0 {
		if len(args) != 1 || args[0] != "update" {
			fatal(fmt.Errorf("unknown command %q (try --help)", args[0]))
		}
		if err := update.Run(context.Background(), server.Version, os.Stdout); err != nil {
			fatal(err)
		}
		return
	}

	if *showVersion {
		fmt.Println("rh-sc-mcp " + server.Version)
		return
	}

	resolvedPath, err := config.ResolvePath(*cfgPath)
	if err != nil {
		fatal(err)
	}
	cfg, err := config.Load(resolvedPath)
	if err != nil {
		fatal(err)
	}

	// --http is applied after Load, so the checks Load ran never saw it:
	// a config with no bearer token is valid right up until this flag
	// turns the transport on. OverrideHTTPAddr re-runs the same rules
	// rather than leaving them to be re-stated here and drift.
	if err := cfg.OverrideHTTPAddr(*httpAddr); err != nil {
		fatal(err)
	}

	srv := server.New(cfg)

	// stderr, not stdout: stdout is the MCP transport and must carry
	// nothing but protocol frames.
	for _, w := range cfg.Warnings() {
		fmt.Fprintln(os.Stderr, "rh-sc-mcp: warning: "+w)
	}

	if *printScope {
		printResolvedScope(srv)
		return
	}

	if *checkAuth {
		ctx, cancel := context.WithTimeout(context.Background(),
			time.Duration(cfg.TimeoutSeconds)*2*time.Second)
		defer cancel()
		res := srv.CheckAuth(ctx)
		b, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			fatal(err)
		}
		fmt.Println(string(b))
		// A non-zero exit so this is usable as a health check in a
		// script, not only read by a person.
		if !res.OK() {
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// stderr in both modes: on stdio, stdout is the transport.
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	var runErr error
	if cfg.HTTP.Enabled() {
		runErr = srv.ServeHTTP(ctx, logger)
	} else {
		runErr = srv.Run(ctx)
	}
	if runErr != nil && ctx.Err() == nil {
		fatal(runErr)
	}
}

// printResolvedScope reports the effective configuration.
//
// The payload is built in the server package so it can be tested; the
// one thing that matters about it is that it contains no credential,
// and the usual destination for this output is a bug report.
func printResolvedScope(srv *server.Server) {
	b, err := json.MarshalIndent(srv.ScopeReport(), "", "  ")
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(b))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "rh-sc-mcp: "+err.Error())
	os.Exit(1)
}
