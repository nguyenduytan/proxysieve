package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/nguyenduytan/proxysieve/internal/app"
	"github.com/nguyenduytan/proxysieve/internal/buildinfo"
	"github.com/nguyenduytan/proxysieve/internal/configload"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func runStart(args []string, stdout, stderr io.Writer, home string, env map[string]string) int {
	f := flag.NewFlagSet("start", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	file := f.String("file", "", "Configuration file")
	if f.Parse(args) != nil || f.NArg() != 0 {
		return usageError(stderr)
	}
	options := configload.Options{Home: home, Env: env}
	if *file != "" {
		r, err := os.Open(*file)
		if err != nil {
			_, _ = io.WriteString(stderr, "CONFIG_READ_FAILED\n")
			return 1
		}
		defer func() { _ = r.Close() }()
		options.File = r
	}
	effective, err := configload.Load(options)
	if err != nil {
		_, _ = io.WriteString(stderr, "CONFIG_INVALID\n")
		return 1
	}
	runtime, err := app.Build(effective.Config)
	if errors.Is(err, app.ErrUnsupportedListener) {
		_, _ = io.WriteString(stderr, "RUNTIME_UNSUPPORTED: the configured listener or control-plane feature is unavailable.\n")
		return 1
	}
	if err != nil {
		_, _ = io.WriteString(stderr, "RUNTIME_BUILD_FAILED\n")
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ready := func() error {
		if _, err := fmt.Fprintf(stdout, "ProxySieve %s — Tony Nguyen\nHTTP %s\nSOCKS5 %s\nAdmin %s\nRouting: configured policies; no implicit DIRECT\nStatus: ready\n", buildinfo.Current().Version, displayBind(runtime.Bind), displayBind(runtime.SOCKSBind), displayAdmin(runtime.AdminBind)); err != nil {
			return err
		}
		if runtime.SetupToken != "" {
			_, err := fmt.Fprintf(stdout, "\nFirst-run setup token (expires in 10 minutes): %s\n", runtime.SetupToken)
			return err
		}
		return nil
	}
	if err = runtime.RunReady(ctx, ready); err != nil {
		_, _ = io.WriteString(stderr, "GATEWAY_FAILED\n")
		return 1
	}
	return 0
}
func displayBind(value string) string {
	if value == "" {
		return "disabled"
	}
	return value
}
func displayAdmin(value string) string {
	if value == "" {
		return "disabled"
	}
	return "http://" + value
}
