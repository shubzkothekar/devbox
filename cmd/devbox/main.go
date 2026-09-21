// Command devbox is the host-side CLI for creating and configuring DevBox
// projects.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shubzkothekar/devbox/internal/plugins"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "devbox:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: devbox create <container-name> | devbox plugin <command>")
	}

	switch args[0] {
	case "create":
		if len(args) < 2 {
			return errors.New("usage: devbox create <container-name>")
		}
		return fmt.Errorf("unsupported command %q", args[0])

	case "plugin":
		return runPlugin(args[1:], stdout, stderr)

	default:
		return fmt.Errorf("unsupported command %q", args[0])
	}
}

func runPlugin(args []string, stdout, stderr io.Writer) error {
	var projectRoot string
	var posArgs []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--project-root" {
			if i+1 >= len(args) {
				return errors.New("usage: devbox plugin resolve [--project-root <path>]")
			}
			i++
			projectRoot = args[i]
		} else if strings.HasPrefix(arg, "--project-root=") {
			projectRoot = strings.TrimPrefix(arg, "--project-root=")
		} else {
			posArgs = append(posArgs, arg)
		}
	}

	if len(posArgs) == 0 {
		return errors.New("usage: devbox plugin <command>")
	}

	if projectRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		projectRoot = cwd
	} else {
		abs, err := filepath.Abs(projectRoot)
		if err != nil {
			return err
		}
		projectRoot = abs
	}

	ctx := context.Background()
	service := plugins.NewService(projectRoot)

	switch posArgs[0] {
	case "list":
		if len(posArgs) > 1 {
			return errors.New("usage: devbox plugin list")
		}
		catalog, err := service.List(ctx)
		if err != nil {
			return err
		}
		mode, err := service.SourceMode(ctx)
		if err != nil {
			return err
		}
		return printCatalog(stdout, mode, catalog)

	case "installed":
		if len(posArgs) > 1 {
			return errors.New("usage: devbox plugin installed")
		}
		installed, err := service.Installed(ctx)
		if err != nil {
			return err
		}
		mode, err := service.SourceMode(ctx)
		if err != nil {
			return err
		}
		return printInstalled(stdout, mode, installed)

	case "install":
		if len(posArgs) != 2 {
			return errors.New("usage: devbox plugin install <plugin-id>")
		}
		id := posArgs[1]
		if err := service.Install(ctx, id); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Installed plugin %q\n", id)
		return nil

	case "uninstall":
		if len(posArgs) != 2 {
			return errors.New("usage: devbox plugin uninstall <plugin-id>")
		}
		id := posArgs[1]
		if err := service.Uninstall(ctx, id); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Uninstalled plugin %q\n", id)
		return nil

	case "update":
		if len(posArgs) > 2 {
			return errors.New("usage: devbox plugin update [plugin-id]")
		}
		var id string
		if len(posArgs) == 2 {
			id = posArgs[1]
		}
		if err := service.Update(ctx, id); err != nil {
			return err
		}
		if id != "" {
			fmt.Fprintf(stdout, "Updated plugin %q\n", id)
		} else {
			fmt.Fprintf(stdout, "Updated all plugins\n")
		}
		return nil

	case "resolve":
		if len(posArgs) > 1 {
			return errors.New("usage: devbox plugin resolve [--project-root <path>]")
		}
		plan, err := service.ResolveGenerated(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Resolved %d plugin(s)\n", len(plan.Plugins))
		return nil

	default:
		return dispatchPluginCommand(ctx, posArgs, stdout, service)
	}
}

func dispatchPluginCommand(ctx context.Context, args []string, stdout io.Writer, service plugins.Service) error {
	if len(args) < 2 {
		return errors.New("usage: devbox plugin <plugin-id> <command> [args...]")
	}
	pluginID := args[0]
	command := args[1]
	_, _, err := service.FindCommand(ctx, pluginID, command)
	if err != nil {
		return err
	}
	// Route plugin command dispatch metadata through a later exec implementation;
	// this task only resolves and validates command identity.
	return nil
}

func printCatalog(w io.Writer, mode string, catalog []plugins.CatalogPlugin) error {
	fmt.Fprintf(w, "Registry source mode: %s\n", mode)
	for _, p := range catalog {
		if p.Description != "" {
			fmt.Fprintf(w, "%s (%s) - %s\n", p.ID, p.Version, p.Description)
		} else if p.Version != "" {
			fmt.Fprintf(w, "%s (%s)\n", p.ID, p.Version)
		} else {
			fmt.Fprintf(w, "%s\n", p.ID)
		}
	}
	return nil
}

func printInstalled(w io.Writer, mode string, installed []plugins.InstalledPlugin) error {
	fmt.Fprintf(w, "Registry source mode: %s\n", mode)
	for _, p := range installed {
		if p.Version != "" {
			fmt.Fprintf(w, "%s (%s)\n", p.ID, p.Version)
		} else {
			fmt.Fprintf(w, "%s\n", p.ID)
		}
	}
	return nil
}
