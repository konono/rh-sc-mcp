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
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/konono/rh-sc-mcp/internal/config"
	"github.com/konono/rh-sc-mcp/internal/server"
)

func main() {
	var (
		cfgPath     = flag.String("config", os.Getenv("RH_SC_MCP_CONFIG"), "path to the configuration YAML (also $RH_SC_MCP_CONFIG)")
		printScope  = flag.Bool("print-scope", false, "print the resolved scope and the tools it exposes, then exit")
		checkAuth   = flag.Bool("check-auth", false, "exchange the offline token and probe the API, then exit; prints no credential")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("rh-sc-mcp " + server.Version)
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fatal(err)
	}

	srv := server.New(cfg)

	// stderr, not stdout: stdout is the MCP transport and must carry
	// nothing but protocol frames.
	for _, w := range cfg.Warnings() {
		fmt.Fprintln(os.Stderr, "rh-sc-mcp: warning: "+w)
	}

	if *printScope {
		printResolvedScope(cfg, srv)
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

	if err := srv.Run(ctx); err != nil && ctx.Err() == nil {
		fatal(err)
	}
}

// printResolvedScope reports the effective configuration.
//
// It deliberately prints no credential, not even a fingerprint of one:
// the usual place this output ends up is a bug report.
func printResolvedScope(cfg *config.Config, srv *server.Server) {
	resources := map[string]string{}
	for _, r := range config.AllResources {
		resources[string(r)] = cfg.LevelFor(r).String()
	}
	// An empty list rather than null: this output is read by people and
	// by scripts, and "warnings": null invites a nil dereference in both.
	warnings := cfg.Warnings()
	if warnings == nil {
		warnings = []string{}
	}
	out := map[string]any{
		"endpoint":    cfg.BaseURL,
		"apiVersion":  cfg.APIVersion,
		"ssoTokenURL": cfg.SSOTokenURL,
		"ssoClientID": cfg.SSOClientID,
		"readOnly":    cfg.IsReadOnly(),
		"resources":   resources,
		"tools":       srv.Registered(),
		"maxResults":  cfg.MaxResults,
		"maxComments": cfg.MaxComments,
		"timeoutSec":  cfg.TimeoutSeconds,
		"warnings":    warnings,
	}
	if cfg.CaseViewURL != "" {
		out["caseViewURL"] = cfg.CaseViewURL
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(b))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "rh-sc-mcp: "+err.Error())
	os.Exit(1)
}
