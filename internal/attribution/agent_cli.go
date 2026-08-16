package attribution

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var agentMCPNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type agentCLIOptions struct {
	project string
	host    string
	name    string
}

func runAgentCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		printAgentUsage(stderr)
		return 2
	}
	subcommand := args[1]
	options, err := parseAgentCLIOptions(subcommand, args[2:])
	if err != nil {
		fmt.Fprintln(stderr, err)
		printAgentUsage(stderr)
		return 2
	}
	root, err := filepath.Abs(options.project)
	if err != nil {
		fmt.Fprintln(stderr, "resolve project:", err)
		return 2
	}

	switch subcommand {
	case "setup":
		registeredName, err := setupCodexAgent(root, options, os.Executable, runAgentSetupCommand)
		if err != nil {
			return renderCLIError(err, stderr)
		}
		fmt.Fprintf(stdout, "Registered the project-bound Attribution MCP server as %q in Codex.\n", registeredName)
		fmt.Fprintln(stdout, "The access token remains in macOS Keychain and is never placed in MCP configuration or tool data.")
		fmt.Fprintln(stdout, "Start a new Codex session in this project to use the four Attribution live-check tools.")
		return 0
	case "serve":
		if err := serveAgentMCP(root, OSKeychainTokenStore{}, os.Stdin, stdout); err != nil {
			return renderCLIError(err, stderr)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown agent command %q\n", subcommand)
		printAgentUsage(stderr)
		return 2
	}
}

func parseAgentCLIOptions(subcommand string, args []string) (agentCLIOptions, error) {
	if subcommand != "setup" && subcommand != "serve" {
		return agentCLIOptions{}, fmt.Errorf("unknown agent command %q", subcommand)
	}
	options := agentCLIOptions{
		project: ".",
		host:    "codex",
	}
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--project":
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
				return agentCLIOptions{}, errors.New("--project requires a directory")
			}
			index++
			options.project = args[index]
		case "--host":
			if subcommand != "setup" {
				return agentCLIOptions{}, errors.New("--host is supported only by agent setup")
			}
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
				return agentCLIOptions{}, errors.New("--host requires codex")
			}
			index++
			options.host = args[index]
		case "--name":
			if subcommand != "setup" {
				return agentCLIOptions{}, errors.New("--name is supported only by agent setup")
			}
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
				return agentCLIOptions{}, errors.New("--name requires a value")
			}
			index++
			options.name = args[index]
		default:
			return agentCLIOptions{}, fmt.Errorf("unknown option %q", args[index])
		}
	}
	if options.host != "codex" {
		return agentCLIOptions{}, errors.New("agent setup currently supports only --host codex")
	}
	if options.name != "" && !agentMCPNamePattern.MatchString(options.name) {
		return agentCLIOptions{}, errors.New("--name must contain only letters, numbers, underscores, or hyphens (maximum 64 characters)")
	}
	return options, nil
}

type agentExecutable func() (string, error)
type agentSetupCommand func(name string, args ...string) ([]byte, error)

func runAgentSetupCommand(name string, args ...string) ([]byte, error) {
	command := exec.Command(name, args...)
	return command.CombinedOutput()
}

func setupCodexAgent(root string, options agentCLIOptions, executable agentExecutable, run agentSetupCommand) (string, error) {
	binding, err := ReadCloudBinding(root)
	if err != nil {
		return "", err
	}
	config, _, err := ReadConfig(root)
	if err != nil {
		return "", err
	}
	if config.App.BundleID != binding.BundleID {
		return "", fmt.Errorf("%s is linked to %s but %s configures %s; run `attribution connect` again", CloudConfigPath, binding.BundleID, ConfigPath, config.App.BundleID)
	}
	registeredName := options.name
	if registeredName == "" {
		registeredName, err = agentMCPNameForBinding(binding)
		if err != nil {
			return "", err
		}
	}
	binary, err := executable()
	if err != nil {
		return "", fmt.Errorf("resolve attribution executable: %w", err)
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return "", fmt.Errorf("resolve attribution executable: %w", err)
	}
	arguments := []string{
		"mcp", "add", registeredName, "--", binary,
		"agent", "serve", "--project", root,
	}
	output, err := run("codex", arguments...)
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 512 {
			message = message[:512]
		}
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("register Codex MCP server: %s", message)
	}
	return registeredName, nil
}

func agentMCPNameForBinding(binding CloudBinding) (string, error) {
	reference, _, err := cloudCredentialReference(binding.BaseURL, binding.OrganizationID, binding.ApplicationID, binding.BundleID)
	if err != nil {
		return "", err
	}
	digest := strings.TrimPrefix(reference, cloudCredentialPrefix)
	if len(digest) < 12 {
		return "", errors.New("cloud credential reference is invalid")
	}
	return "attribution-" + digest[:12], nil
}

func printAgentUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: attribution agent setup [--project <dir>] [--host codex] [--name <name>]")
	fmt.Fprintln(writer, "       attribution agent serve [--project <dir>]")
}
