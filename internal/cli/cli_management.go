package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"

	"github.com/knothhe/picup/internal/install"
)

func (r Runner) manageCLI(ctx context.Context, args []string) error {
	if len(args) == 0 || (args[0] != "status" && args[0] != "uninstall") {
		return errors.New("usage: picup cli status|uninstall [--dir DIRECTORY] [--output text|json]")
	}
	flags := flag.NewFlagSet("picup cli "+args[0], flag.ContinueOnError)
	flags.SetOutput(r.Err)
	directory, output := install.DefaultDir(), "text"
	flags.StringVar(&directory, "dir", directory, "")
	flags.StringVar(&output, "output", output, "")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || output != "text" && output != "json" {
		return errors.New("use --dir DIRECTORY and --output text|json")
	}
	if args[0] == "uninstall" {
		if err := install.UninstallCLI(ctx, directory); err != nil {
			return err
		}
	}
	status := install.InspectCLI(ctx, directory)
	diagnostic := ""
	if status.Err != nil {
		diagnostic = status.Err.Error()
	}
	if output == "json" {
		return json.NewEncoder(r.Out).Encode(struct {
			State        install.CLIInstallationState `json:"state"`
			Path         string                       `json:"path"`
			Version      string                       `json:"version,omitempty"`
			CommandPath  string                       `json:"command_path,omitempty"`
			InPath       bool                         `json:"in_path"`
			CanUninstall bool                         `json:"can_uninstall"`
			Error        string                       `json:"error,omitempty"`
		}{status.State, status.Path, status.Version, status.CommandPath, status.InPath, status.CanUninstall, diagnostic})
	}
	fmt.Fprintf(r.Out, "State: %s\nPath: %s\n", status.State, status.Path)
	if status.Version != "" {
		fmt.Fprintln(r.Out, "Version:", status.Version)
	}
	fmt.Fprintln(r.Out, "On PATH:", status.InPath)
	if status.CommandPath != "" {
		fmt.Fprintln(r.Out, "Resolved command:", status.CommandPath)
	}
	if diagnostic != "" {
		fmt.Fprintln(r.Err, diagnostic)
	}
	return nil
}
