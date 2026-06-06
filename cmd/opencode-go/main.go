package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/opencode-go/opencode-go/internal/config"
	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/server"
	"github.com/opencode-go/opencode-go/internal/tool"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "serve":
		if err := runServe(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "models":
		if err := runModels(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: opencode-go serve [--hostname 127.0.0.1] [--port N]")
	fmt.Fprintln(os.Stderr, "       opencode-go models")
}

// runModels prints one "providerID/modelID" line per configured model to
// STDOUT (no spaces, no headers), so tg-bot-go's fetchOpencodeModels can parse
// it. Logs/errors go to STDERR. Output is sorted for deterministic results.
func runModels(args []string) error {
	_ = args

	workdir := os.Getenv("OPENCODE_GO_WORKDIR")
	if workdir == "" {
		if wd, err := os.Getwd(); err == nil {
			workdir = wd
		}
	}

	cfg := config.Load(workdir)
	reg := provider.BuildRegistry(cfg)

	for _, p := range reg.Providers {
		modelIDs := make([]string, 0, len(p.Models))
		for modelID := range p.Models {
			modelIDs = append(modelIDs, modelID)
		}
		sort.Strings(modelIDs)
		for _, modelID := range modelIDs {
			fmt.Println(p.ID + "/" + modelID)
		}
	}

	return nil
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	hostname := fs.String("hostname", "127.0.0.1", "bind hostname (127.0.0.1 only)")
	port := fs.Int("port", 4096, "bind port")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Security posture: bind 127.0.0.1 only (architecture §11). Refuse other
	// interfaces.
	if *hostname != "127.0.0.1" && *hostname != "localhost" {
		return fmt.Errorf("refusing to bind non-loopback hostname %q (127.0.0.1 only)", *hostname)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	prov, model, err := buildProvider(logger)
	if err != nil {
		return err
	}

	workdir := os.Getenv("OPENCODE_GO_WORKDIR")
	if workdir == "" {
		if wd, err := os.Getwd(); err == nil {
			workdir = wd
		} else {
			workdir = "."
		}
	}

	srv := server.New(server.Options{
		Provider: prov,
		Model:    model,
		Logger:   logger,
		Tools:    tool.NewDefaultRegistry(),
		Workdir:  workdir,
	})

	addr := fmt.Sprintf("%s:%d", *hostname, *port)
	logger.Info("opencode-go listening", "addr", addr, "auth", "none (loopback only)")

	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe(addr)
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case <-sig:
		logger.Info("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

// buildProvider constructs the provider from env. MOCK takes precedence when
// OPENCODE_GO_MOCK=1 so M1 is testable without real tokens.
func buildProvider(logger *slog.Logger) (provider.Provider, string, error) {
	if os.Getenv("OPENCODE_GO_MOCK") == "1" {
		logger.Info("using MOCK provider (OPENCODE_GO_MOCK=1)")
		return provider.NewMock(""), "mock", nil
	}

	baseURL := os.Getenv("OPENCODE_GO_BASE_URL")
	apiKey := os.Getenv("OPENCODE_GO_API_KEY")
	model := os.Getenv("OPENCODE_GO_MODEL")

	if apiKey != "" && baseURL != "" {
		// Model may be "providerID/modelID" or just a model string; the
		// OpenAI-compatible client only needs the modelID it sends on the wire.
		modelID := model
		if i := strings.Index(model, "/"); i >= 0 {
			modelID = model[i+1:]
		}
		return provider.NewOpenAI("openai", baseURL, apiKey, modelID, &http.Client{Timeout: 0}), modelID, nil
	}

	// Env vars unset: try auto-config from the opencode config + auth.json so
	// the bot (which launches us without OPENCODE_GO_* vars) gets a REAL
	// provider instead of the mock.
	workdir := os.Getenv("OPENCODE_GO_WORKDIR")
	if workdir == "" {
		if wd, err := os.Getwd(); err == nil {
			workdir = wd
		}
	}
	cfg := config.Load(workdir)
	if cfgBaseURL, cfgAPIKey, providerID, modelID, ok := provider.ResolveDefault(cfg); ok {
		logger.Info("using provider from opencode config", "provider", providerID, "model", modelID)
		return provider.NewOpenAI("openai", cfgBaseURL, cfgAPIKey, modelID, &http.Client{Timeout: 0}), modelID, nil
	}

	logger.Warn("no provider configured (set OPENCODE_GO_BASE_URL + OPENCODE_GO_API_KEY, or OPENCODE_GO_MOCK=1, or an opencode config default model); falling back to MOCK")
	return provider.NewMock(""), "mock", nil
}
