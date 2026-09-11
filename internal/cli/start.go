package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/nguyenduytan/proxysieve/internal/app"
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
		_, _ = io.WriteString(stderr, "LISTENER_UNAVAILABLE: remove SOCKS5 listeners until M4 is implemented.\n")
		return 1
	}
	if err != nil {
		_, _ = io.WriteString(stderr, "RUNTIME_BUILD_FAILED\n")
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "ProxySieve %s\nHTTP %s\nPolicy: %s\nStatus: ready\n", buildVersion(), runtime.Bind, routeStatus(effective.Config.Security.AllowDirect))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err = runtime.Run(ctx); err != nil {
		_, _ = io.WriteString(stderr, "GATEWAY_FAILED\n")
		return 1
	}
	return 0
}
func buildVersion() string { return "foundation" }
func routeStatus(allow bool) string {
	if allow {
		return "explicit DIRECT allowlist only"
	}
	return "fail closed"
}
