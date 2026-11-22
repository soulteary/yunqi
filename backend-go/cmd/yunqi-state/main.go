// yunqi-state manages offline recovery bundles without starting a service.
package main

import (
	"flag"
	"fmt"
	"github.com/soulteary/yunqi/internal/statebundle"
	"io"
	"os"
)

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: yunqi-state backup|verify|restore [options]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(out)
	config := flags.String("config", ".config/config.json", "configuration file")
	state := flags.String("statedir", ".config", "runtime state directory")
	secrets := flags.String("secrets-file", "", "optional environment secret file (included verbatim)")
	external := flags.String("external-snapshot", "", "operator reference to separately backed-up external state")
	bundle := flags.String("bundle", "", "existing bundle directory")
	output := flags.String("output", "", "new output directory; must not exist")
	offline := flags.Bool("offline", false, "confirm all writers, including older binaries, are stopped")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	switch args[0] {
	case "backup":
		if !*offline || *output == "" {
			return fmt.Errorf("backup requires --offline and --output; never stops a service")
		}
		if err := statebundle.Backup(statebundle.Options{ConfigPath: *config, StateDir: *state, SecretsPath: *secrets, ExternalSnapshot: *external}, *output); err != nil {
			return err
		}
	case "verify":
		if *bundle == "" {
			return fmt.Errorf("verify requires --bundle")
		}
		m, err := statebundle.Verify(*bundle)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "verified %d files; external services are not included\n", len(m.Files))
		return nil
	case "restore":
		if *bundle == "" || *output == "" {
			return fmt.Errorf("restore requires --bundle and a new --output directory")
		}
		if err := statebundle.Restore(*bundle, *output); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown command")
	}
	_, _ = fmt.Fprintln(out, "completed; no service was started or reloaded")
	return nil
}
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "state operation failed:", err)
		os.Exit(1)
	}
}
