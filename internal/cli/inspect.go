package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	internalinspect "github.com/nguyenduytan/proxysieve/internal/inspect"
)

func runInspect(args []string, stdout, stderr io.Writer, home string, env map[string]string) int {
	if len(args) < 2 || args[0] != "ca" || args[1] != "init" && args[1] != "fingerprint" && args[1] != "export" && args[1] != "rotate" {
		return usageError(stderr)
	}
	command := args[1]
	flags := flag.NewFlagSet("inspect ca "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	file := flags.String("file", "", "Configuration file")
	path := flags.String("path", "", "CA certificate export path")
	if flags.Parse(args[2:]) != nil || flags.NArg() != 0 || command == "export" && *path == "" || command != "export" && *path != "" {
		return usageError(stderr)
	}
	effective, err := loadEffectiveConfig(*file, home, env)
	if err != nil {
		_, _ = io.WriteString(stderr, "CONFIG_INVALID\n")
		return 1
	}
	switch command {
	case "init", "rotate":
		info, createErr := internalinspect.Create(effective.Config.Server.DataDir, command == "rotate")
		if createErr == nil {
			_, err = fmt.Fprintf(stdout, "Inspect CA %s. SHA256 fingerprint: %s\n", map[bool]string{true: "rotated", false: "created"}[command == "rotate"], info.Fingerprint)
		} else {
			err = createErr
		}
	case "fingerprint":
		info, readErr := internalinspect.ReadInfo(effective.Config.Server.DataDir)
		if readErr == nil {
			_, err = fmt.Fprintf(stdout, "%s\n", info.Fingerprint)
		} else {
			err = readErr
		}
	case "export":
		err = internalinspect.Export(effective.Config.Server.DataDir, *path)
		if err == nil {
			_, err = fmt.Fprintf(stdout, "Inspect CA certificate exported: %s\n", *path)
		}
	}
	if err == nil {
		return 0
	}
	switch {
	case errors.Is(err, internalinspect.ErrExists):
		_, _ = io.WriteString(stderr, "INSPECT_CA_EXISTS: use rotate explicitly or choose a new export path.\n")
	case errors.Is(err, internalinspect.ErrNotFound):
		_, _ = io.WriteString(stderr, "INSPECT_CA_NOT_FOUND: run inspect ca init first.\n")
	case errors.Is(err, internalinspect.ErrInvalid):
		_, _ = io.WriteString(stderr, "INSPECT_CA_INVALID: CA data or file permissions are unsafe.\n")
	default:
		_, _ = io.WriteString(stderr, "INSPECT_CA_UNAVAILABLE\n")
	}
	return 1
}
