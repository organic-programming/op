package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/organic-programming/grace-op/internal/who"

	"google.golang.org/protobuf/proto"
)

func cmdWho(format Format, verb string, args []string) int {
	switch verb {
	case "list":
		return cmdWhoList(format, args)
	case "show":
		return cmdWhoShow(format, args)
	case "new":
		return cmdWhoNew(format, args)
	default:
		fmt.Fprintf(os.Stderr, "op %s: unsupported identity verb\n", verb)
		return 1
	}
}

func cmdWhoList(format Format, args []string) int {
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "usage: op list [root]")
		return 1
	}

	root := "."
	if len(args) == 1 {
		root = args[0]
	}

	resp, err := who.List(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op list: %v\n", err)
		return 1
	}

	printFormattedResponse(format, resp)
	return 0
}

func cmdWhoShow(format Format, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: op show <uuid-or-prefix>")
		return 1
	}

	resp, err := who.Show(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "op show: %v\n", err)
		return 1
	}

	printFormattedResponse(format, resp)
	return 0
}

func cmdWhoNew(format Format, args []string) int {
	payload, err := whoNewPayload(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op new: %v\n", err)
		return 1
	}

	var resp proto.Message
	if payload == "" {
		created, createErr := who.CreateInteractive(os.Stdin, os.Stdout)
		if createErr != nil {
			fmt.Fprintf(os.Stderr, "op new: %v\n", createErr)
			return 1
		}
		resp = created
	} else {
		created, createErr := who.CreateFromJSON(payload)
		if createErr != nil {
			fmt.Fprintf(os.Stderr, "op new: %v\n", createErr)
			return 1
		}
		resp = created
	}

	printFormattedResponse(format, resp)
	return 0
}

func whoNewPayload(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}

	if len(args) == 1 && looksLikeJSON(args[0]) {
		return args[0], nil
	}

	switch {
	case args[0] == "--json":
		if len(args) != 2 {
			return "", fmt.Errorf("usage: op new [--json <payload>]")
		}
		return args[1], nil
	case strings.HasPrefix(args[0], "--json="):
		if len(args) != 1 {
			return "", fmt.Errorf("usage: op new [--json <payload>]")
		}
		return strings.TrimPrefix(args[0], "--json="), nil
	default:
		return "", fmt.Errorf("usage: op new [--json <payload>]")
	}
}

func printFormattedResponse(format Format, resp proto.Message) {
	if resp == nil {
		return
	}
	out := strings.TrimSpace(FormatResponse(format, resp))
	if out != "" {
		fmt.Println(out)
	}
}
