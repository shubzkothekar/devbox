// Package main provides the devbox CLI for creating and configuring DevBox
// projects.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shubzkothekar/devbox/internal/config"
	"github.com/shubzkothekar/devbox/internal/plugins"
	"github.com/shubzkothekar/devbox/internal/project"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		var procExitErr *exec.ExitError
		if errors.As(err, &procExitErr) {
			os.Exit(procExitErr.ExitCode())
		}
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
		return runCreate(args[1:], stdout, stderr)

	case "plugin":
		return runPlugin(args[1:], stdout, stderr)

	default:
		return fmt.Errorf("unsupported command %q", args[0])
	}
}

func runCreate(args []string, stdout, stderr io.Writer) error {
	var name string
	var destination string
	var ref string
	var seenExtraPositional bool

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			if seenExtraPositional {
				return errors.New("usage: devbox create <container-name> [--destination <path>] [--ref <git-ref>]")
			}
			switch {
			case arg == "--destination":
				if i+1 >= len(args) {
					return errors.New("usage: devbox create <container-name> [--destination <path>] [--ref <git-ref>]")
				}
				i++
				destination = args[i]
			case strings.HasPrefix(arg, "--destination="):
				destination = strings.TrimPrefix(arg, "--destination=")
			case arg == "--ref":
				if i+1 >= len(args) {
					return errors.New("usage: devbox create <container-name> [--destination <path>] [--ref <git-ref>]")
				}
				i++
				ref = args[i]
			case strings.HasPrefix(arg, "--ref="):
				ref = strings.TrimPrefix(arg, "--ref=")
			default:
				return fmt.Errorf("unknown flag %q", arg)
			}
		} else {
			if name == "" {
				name = arg
			} else {
				seenExtraPositional = true
			}
		}
	}

	if name == "" || seenExtraPositional {
		return errors.New("usage: devbox create <container-name> [--destination <path>] [--ref <git-ref>]")
	}

	result, err := project.Create(context.Background(), project.CreateRequest{
		Name:        name,
		Destination: destination,
		Ref:         ref,
	})
	if err != nil {
		return err
	}

	displayDest := destination
	if displayDest == "" {
		displayDest = "./" + name
	}

	fmt.Fprintf(stdout, "Created DevBox project: %s\n", result.Root)
	fmt.Fprintf(stdout, "Next:\n")
	fmt.Fprintf(stdout, "  cd %s\n", displayDest)
	fmt.Fprintf(stdout, "  devbox plugin install <plugin-id>\n")
	return nil
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

	case "hook-env":
		return runHookEnv(posArgs[1:], stdout, stderr)

	case "exec":
		return runPluginExec(posArgs[1:], stdout, stderr, projectRoot)

	default:
		return runPluginExec(posArgs, stdout, stderr, projectRoot)
	}
}

func runHookEnv(args []string, stdout, stderr io.Writer) error {
	var pluginJSON string
	var cmdArgs []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			cmdArgs = append(cmdArgs, args[i+1:]...)
			break
		}
		if arg == "--plugin-json" {
			if i+1 >= len(args) {
				return errors.New("usage: devbox plugin hook-env --plugin-json <json> -- <command> [args...]")
			}
			i++
			pluginJSON = args[i]
		} else if strings.HasPrefix(arg, "--plugin-json=") {
			pluginJSON = strings.TrimPrefix(arg, "--plugin-json=")
		} else {
			cmdArgs = append(cmdArgs, arg)
		}
	}

	if pluginJSON == "" || len(cmdArgs) == 0 {
		return errors.New("usage: devbox plugin hook-env --plugin-json <json> -- <command> [args...]")
	}

	var plugin config.ResolvedPlugin
	if err := json.Unmarshal([]byte(pluginJSON), &plugin); err != nil {
		return fmt.Errorf("invalid plugin json: %w", err)
	}

	envVars := plugins.OptionEnvVars(plugin.ID, plugin.Options)
	envVars = append(envVars, "DEVBOX_PLUGIN_ID="+plugin.ID)
	if plugin.Root != "" {
		envVars = append(envVars, "DEVBOX_PLUGIN_ROOT="+plugin.Root)
	}

	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(), envVars...)
	return cmd.Run()
}

func runPluginExec(args []string, stdout, stderr io.Writer, projectRoot string) error {
	var planPath string
	var posArgs []string
	var extraArgs []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			extraArgs = append(extraArgs, args[i+1:]...)
			break
		}
		if arg == "--plan" {
			if i+1 >= len(args) {
				return errors.New("usage: devbox plugin exec [--plan <path>] <plugin-id> <command> [--] [args...]")
			}
			i++
			planPath = args[i]
		} else if strings.HasPrefix(arg, "--plan=") {
			planPath = strings.TrimPrefix(arg, "--plan=")
		} else {
			posArgs = append(posArgs, arg)
		}
	}

	if len(posArgs) < 2 {
		return errors.New("usage: devbox plugin <plugin-id> <command> [args...]")
	}

	pluginID := posArgs[0]
	commandName := posArgs[1]
	if len(posArgs) > 2 {
		extraArgs = append(posArgs[2:], extraArgs...)
	}

	var targetPlugin config.ResolvedPlugin
	var targetCmd config.ResolvedCommand
	var found bool

	if planPath == "" {
		if envPlan := os.Getenv("DEVBOX_PLUGIN_PLAN"); envPlan != "" {
			planPath = envPlan
		} else {
			candidateProject := filepath.Join(projectRoot, ".generated", "plugins", "plan.json")
			if _, err := os.Stat(candidateProject); err == nil {
				planPath = candidateProject
			} else if _, err := os.Stat("/opt/devbox/plugins/plan.json"); err == nil {
				planPath = "/opt/devbox/plugins/plan.json"
			}
		}
	}

	if planPath != "" {
		planBytes, err := os.ReadFile(planPath)
		if err == nil {
			var plan config.ResolvedPlan
			if err := json.Unmarshal(planBytes, &plan); err == nil {
				for _, p := range plan.Plugins {
					if p.ID == pluginID {
						cmd, ok := p.Commands[commandName]
						if ok {
							targetPlugin = p
							targetCmd = cmd
							found = true
							break
						}
						return fmt.Errorf("plugin %q has no command %q", pluginID, commandName)
					}
				}
				if !found {
					return fmt.Errorf("plugin %q is not enabled or not found", pluginID)
				}
			}
		}
	}

	if !found {
		ctx := context.Background()
		service := plugins.NewService(projectRoot)
		p, cmd, err := service.FindCommand(ctx, pluginID, commandName)
		if err != nil {
			return err
		}
		targetPlugin = p
		targetCmd = cmd
	}

	if targetCmd.User == "" {
		targetCmd.User = "devbox"
	}

	actualRoot := targetPlugin.Root
	scriptPath := filepath.Join(actualRoot, targetCmd.Path)
	if _, err := os.Stat(scriptPath); err != nil {
		if planPath != "" {
			altRoot := filepath.Join(filepath.Dir(planPath), targetPlugin.ID)
			altPath := filepath.Join(altRoot, targetCmd.Path)
			if _, err2 := os.Stat(altPath); err2 == nil {
				actualRoot = altRoot
				scriptPath = altPath
			}
		}
	}

	envVars := plugins.OptionEnvVars(targetPlugin.ID, targetPlugin.Options)
	envVars = append(envVars, "DEVBOX_PLUGIN_ID="+targetPlugin.ID)
	if actualRoot != "" {
		envVars = append(envVars, "DEVBOX_PLUGIN_ROOT="+actualRoot)
	}

	var execCmd *exec.Cmd
	if targetCmd.User == "root" {
		if os.Geteuid() == 0 {
			execCmd = exec.Command(scriptPath, extraArgs...)
		} else {
			sudoArgs := append([]string{"-n", "-E", scriptPath}, extraArgs...)
			execCmd = exec.Command("sudo", sudoArgs...)
		}
	} else {
		if os.Geteuid() == 0 && targetCmd.User != "" {
			targetUser := targetCmd.User
			if _, err := exec.LookPath("runuser"); err == nil {
				runuserArgs := append([]string{"-u", targetUser, "--", scriptPath}, extraArgs...)
				execCmd = exec.Command("runuser", runuserArgs...)
			} else {
				execCmd = exec.Command(scriptPath, extraArgs...)
			}
		} else {
			execCmd = exec.Command(scriptPath, extraArgs...)
		}
	}

	execCmd.Stdin = os.Stdin
	execCmd.Stdout = stdout
	execCmd.Stderr = stderr
	execCmd.Env = append(os.Environ(), envVars...)
	return execCmd.Run()
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
