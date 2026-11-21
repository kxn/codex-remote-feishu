// Package teamcli is the thin shell entrypoint for shared product commands.
// It discovers registry metadata and transports typed payloads to the daemon.
package teamcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

type Options struct {
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	Client *Client
}

func RunMain(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	return Run(ctx, Options{Args: args, Stdin: stdin, Stdout: stdout, Stderr: stderr})
}

type invocation struct {
	name, runRef, file    string
	stdin, help, discover bool
}

func parse(args []string) (invocation, error) {
	var call invocation
	if len(args) == 0 {
		call.help = true
		return call, nil
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		call.help = true
		if len(args) == 2 && args[0] == "help" {
			call.name = args[1]
			if _, ok := workcmd.Lookup(call.name); !ok {
				return call, fmt.Errorf("unknown team command %q", call.name)
			}
		} else if len(args) != 1 {
			return call, fmt.Errorf("help accepts at most one command name")
		}
		return call, nil
	}
	if args[0] == "commands" {
		if len(args) != 1 {
			return call, fmt.Errorf("commands accepts no arguments")
		}
		call.discover = true
		return call, nil
	}
	call.name = args[0]
	args = args[1:]
	if _, direct := workcmd.Lookup(call.name); !direct && len(args) > 0 {
		call.name += "." + args[0]
		args = args[1:]
	}
	if _, ok := workcmd.Lookup(call.name); !ok {
		return call, fmt.Errorf("unknown team command %q", call.name)
	}
	seen := map[string]bool{}
	for len(args) > 0 {
		flag, value, inline := strings.Cut(args[0], "=")
		args = args[1:]
		if seen[flag] {
			return call, fmt.Errorf("duplicate flag %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--help", "-h":
			if inline {
				return call, fmt.Errorf("%s does not take a value", flag)
			}
			call.help = true
		case "--stdin":
			if inline {
				return call, fmt.Errorf("--stdin does not take a value")
			}
			call.stdin = true
		case "--run-ref", "--file":
			if !inline {
				if len(args) == 0 || strings.HasPrefix(args[0], "--") {
					return call, fmt.Errorf("%s requires a value", flag)
				}
				value = args[0]
				args = args[1:]
			}
			if value == "" {
				return call, fmt.Errorf("%s requires a nonempty value", flag)
			}
			if flag == "--run-ref" {
				call.runRef = value
			} else {
				call.file = value
			}
		default:
			return call, fmt.Errorf("unknown argument %q", flag)
		}
	}
	if call.stdin && call.file != "" {
		return call, fmt.Errorf("--stdin and --file are mutually exclusive")
	}
	command, _ := workcmd.Lookup(call.name)
	if !call.help && command.CredentialPurpose == "worker" && strings.TrimSpace(call.runRef) == "" {
		return call, fmt.Errorf("--run-ref is required for this invocation")
	}
	if !call.help && command.CredentialPurpose == "bootstrap" && call.runRef != "" {
		return call, fmt.Errorf("bootstrap project creation must not supply --run-ref")
	}
	if !call.help && len(command.InputFields) != 0 && !call.stdin && call.file == "" {
		return call, fmt.Errorf("command payload requires --file <path>, --file -, or --stdin")
	}
	return call, nil
}

func Run(ctx context.Context, opts Options) error {
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	call, err := parse(opts.Args)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(opts.Stdout)
	encoder.SetIndent("", "  ")
	if call.discover {
		return encoder.Encode(workcmd.List())
	}
	if call.help {
		if call.name != "" {
			command, _ := workcmd.Lookup(call.name)
			return encoder.Encode(command)
		}
		return encoder.Encode(map[string]any{"usage": "codex-remote team <command> --run-ref <run_ref> [--file <path>|--stdin]", "commands": workcmd.List(), "help": "Use <command> --help or help <command>; commands prints the registry. Credentials come from the bound work process environment."})
	}
	payload := json.RawMessage(`{}`)
	if call.file != "" || call.stdin {
		var reader io.Reader = opts.Stdin
		if call.file != "" && call.file != "-" {
			file, err := os.Open(call.file)
			if err != nil {
				return fmt.Errorf("read command file: %w", err)
			}
			defer file.Close()
			reader = file
		}
		data, err := io.ReadAll(io.LimitReader(reader, workcmd.MaxRequestBytes+1))
		if err != nil {
			return fmt.Errorf("read command payload: %w", err)
		}
		if len(data) > workcmd.MaxRequestBytes {
			return fmt.Errorf("command payload exceeds %d bytes", workcmd.MaxRequestBytes)
		}
		payload = data
	}
	request := workcmd.DispatchRequest{Name: call.name, RunRef: call.runRef, Payload: payload}
	if err := request.Validate(); err != nil {
		return err
	}
	if call.name == "context" {
		var empty struct{}
		if err := workcmd.DecodePayload(payload, &empty); err != nil {
			return err
		}
	}
	endpoint := opts.Getenv(config.WorkToolEndpointEnv)
	token := opts.Getenv(config.WorkSessionTokenEnv)
	if token == "" {
		return fmt.Errorf("bound work session credential is unavailable")
	}
	client := opts.Client
	if client == nil {
		client = NewClient()
	}
	result, err := client.Execute(ctx, endpoint, token, request)
	if err != nil {
		return err
	}
	// Emit only the application result, preserving its status and receipt fields.
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(result)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("invalid team result JSON")
	}
	return encoder.Encode(value)
}
