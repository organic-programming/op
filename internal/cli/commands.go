// Package cli implements OP's command routing — transport-chain dispatch,
// URI dispatch, and OP's own commands.
package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/organic-programming/go-holons/pkg/transport"
	"github.com/organic-programming/grace-op/internal/grpcclient"
	"github.com/organic-programming/grace-op/internal/server"
	"github.com/organic-programming/sophia-who/pkg/identity"
)

// Run dispatches the command and returns an exit code.
func Run(args []string, version string) int {
	format, args, err := parseGlobalFormat(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op: %v\n", err)
		return 1
	}
	if len(args) == 0 {
		PrintUsage()
		return 1
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	// --- OP's own commands ---
	case "run":
		return cmdRun(rest)
	case "discover":
		return cmdDiscover(format)
	case "serve":
		return cmdServe(rest)
	case "version":
		fmt.Printf("op %s\n", version)
		return 0
	case "help", "--help", "-h":
		PrintUsage()
		return 0

	// --- URI dispatch: grpc://, grpc+stdio://, grpc+unix://, grpc+ws:// ---
	default:
		if strings.HasPrefix(cmd, "grpc://") ||
			strings.HasPrefix(cmd, "grpc+stdio://") ||
			strings.HasPrefix(cmd, "grpc+unix://") ||
			strings.HasPrefix(cmd, "grpc+ws://") ||
			strings.HasPrefix(cmd, "grpc+wss://") {
			return cmdGRPC(format, cmd, rest)
		}
		return cmdHolon(format, cmd, rest)
	}
}

// PrintUsage displays the help text.
func PrintUsage() {
	fmt.Print(`op — the Organic Programming CLI

Global flags (must come before <holon> or URI):
  -f, --format <text|json>              output format for RPC responses (default: text)

Holon dispatch (transport chain):
  op <holon> <command> [args]            dispatch via mem://, stdio://, or tcp://
  op who list [root]                     mapped RPC: ListIdentities
  op who show <uuid>                     mapped RPC: ShowIdentity
  op who pin <uuid>                      mapped RPC: PinVersion
  op who new <json>                      mapped RPC: CreateIdentity

Direct gRPC URI dispatch:
  op grpc://<host:port> <method>         gRPC over TCP (existing server)
  op grpc+stdio://<holon> <method>       gRPC over stdio pipe (ephemeral)
  op grpc+unix://<path> <method>         gRPC over Unix socket
  op grpc+ws://<host:port> <method>      gRPC over WebSocket
  op run <holon>:<port>                  start a holon's gRPC server (TCP)
  op run <holon> --listen <URI>          start with any transport

OP commands:
  op discover                            list available holons
  op serve [--listen tcp://:9090]        start OP's own gRPC server
  op version                             show op version
  op help                                this message
`)
}

// --- OP's own commands ---

type discoverEntry struct {
	UUID         string `json:"uuid"`
	GivenName    string `json:"given_name"`
	FamilyName   string `json:"family_name"`
	Lang         string `json:"lang"`
	Clade        string `json:"clade"`
	Status       string `json:"status"`
	RelativePath string `json:"relative_path"`
	Origin       string `json:"origin"`
}

type discoverOutput struct {
	Entries      []discoverEntry `json:"entries"`
	PathBinaries []string        `json:"path_binaries"`
}

func cmdDiscover(format Format) int {
	const scanRoot = "holons"

	var (
		holons  []identity.Identity
		located []identity.LocatedIdentity
	)
	if _, err := os.Stat(scanRoot); err == nil {
		var scanErr error
		holons, scanErr = identity.FindAll(scanRoot)
		if scanErr != nil {
			fmt.Fprintf(os.Stderr, "op discover: %v\n", scanErr)
			return 1
		}
		located, scanErr = identity.FindAllWithPaths(scanRoot)
		if scanErr != nil {
			fmt.Fprintf(os.Stderr, "op discover: %v\n", scanErr)
			return 1
		}
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "op discover: %v\n", err)
		return 1
	}

	relativePathByUUID := make(map[string]string, len(located))
	for _, h := range located {
		relDir := filepath.Dir(h.Path)
		if rel, err := filepath.Rel(scanRoot, relDir); err == nil {
			relDir = rel
		}
		relativePathByUUID[h.Identity.UUID] = filepath.ToSlash(relDir)
	}

	entries := make([]discoverEntry, 0, len(holons))
	for _, h := range holons {
		entries = append(entries, discoverEntry{
			UUID:         h.UUID,
			GivenName:    h.GivenName,
			FamilyName:   h.FamilyName,
			Lang:         h.Lang,
			Clade:        h.Clade,
			Status:       h.Status,
			RelativePath: relativePathByUUID[h.UUID],
			Origin:       "local",
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		leftName := strings.ToLower(strings.TrimSpace(entries[i].GivenName + " " + entries[i].FamilyName))
		rightName := strings.ToLower(strings.TrimSpace(entries[j].GivenName + " " + entries[j].FamilyName))
		if leftName == rightName {
			return entries[i].UUID < entries[j].UUID
		}
		return leftName < rightName
	})

	pathHolons := discoverInPath()
	sort.Strings(pathHolons)

	if format == FormatJSON {
		payload := discoverOutput{
			Entries:      entries,
			PathBinaries: pathHolons,
		}
		out, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "op discover: %v\n", err)
			return 1
		}
		fmt.Println(string(out))
		return 0
	}

	printDiscoverTable(entries, pathHolons)
	return 0
}

func printDiscoverTable(entries []discoverEntry, pathHolons []string) {
	if len(entries) == 0 {
		fmt.Println("No local holons found in holons/.")
	} else {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tLANG\tCLADE\tSTATUS\tORIGIN\tREL_PATH\tUUID")
		for _, entry := range entries {
			fmt.Fprintf(
				w,
				"%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				discoverDisplayName(entry),
				defaultDash(entry.Lang),
				defaultDash(entry.Clade),
				defaultDash(entry.Status),
				defaultDash(entry.Origin),
				defaultDash(entry.RelativePath),
				defaultDash(entry.UUID),
			)
		}
		_ = w.Flush()
	}

	if len(pathHolons) > 0 {
		fmt.Println("\nIn $PATH:")
		for _, name := range pathHolons {
			fmt.Printf("  %s\n", name)
		}
	}
}

func discoverDisplayName(entry discoverEntry) string {
	name := strings.TrimSpace(entry.GivenName + " " + entry.FamilyName)
	if name == "" {
		return "-"
	}
	return name
}

func cmdServe(args []string) int {
	// Support both --listen <URI> and legacy --port <port>
	listenURI := flagOrDefault(args, "--listen", "")
	if listenURI == "" {
		port := flagOrDefault(args, "--port", "9090")
		listenURI = "tcp://:" + port
	}
	noReflect := flagValue(args, "--no-reflect")
	reflect := noReflect == ""

	if err := server.ListenAndServe(listenURI, reflect); err != nil {
		fmt.Fprintf(os.Stderr, "op serve: %v\n", err)
		return 1
	}
	return 0
}

// cmdRun starts a holon's gRPC server as a background process.
// Usage: op run <holon>:<port>  or  op run <holon> --listen <URI>
func cmdRun(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "op run: requires <holon>:<port> or <holon> --listen <URI>")
		return 1
	}

	// Check for --listen form first
	listenURI := flagValue(args, "--listen")
	var holonName string

	if listenURI != "" {
		// op run <holon> --listen <URI>
		holonName = args[0]
	} else {
		// op run <holon>:<port>  (shorthand for tcp)
		holonPort := args[0]
		parts := strings.SplitN(holonPort, ":", 2)
		if len(parts) != 2 || parts[1] == "" {
			fmt.Fprintln(os.Stderr, "op run: format is <holon>:<port> or <holon> --listen <URI>")
			return 1
		}
		holonName = parts[0]
		listenURI = "tcp://:" + parts[1]
	}

	binary, err := resolveHolon(holonName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op run: holon %q not found\n", holonName)
		return 1
	}

	// Launch: <binary> serve --listen <URI>
	cmd := exec.Command(binary, "serve", "--listen", listenURI)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "op run: %v\n", err)
		return 1
	}

	fmt.Printf("op run: started %s (pid %d) on %s\n", holonName, cmd.Process.Pid, listenURI)
	fmt.Printf("op run: stop with: kill %d\n", cmd.Process.Pid)

	// Detach — the process runs in the background
	if err := cmd.Process.Release(); err != nil {
		fmt.Fprintf(os.Stderr, "op run: warning: could not detach process: %v\n", err)
	}

	return 0
}

// cmdGRPC handles gRPC URI dispatching.
//
// Transport schemes:
//   - grpc://host:port <method>       → TCP to existing server
//   - grpc://host:port                → list available methods
//   - grpc://holon <method>           → ephemeral TCP: start binary, call, stop
//   - grpc+stdio://holon <method>     → stdio pipe: launch, pipe, call, done
//   - grpc+unix://path <method>       → Unix domain socket connection
func cmdGRPC(format Format, uri string, args []string) int {
	switch {
	case strings.HasPrefix(uri, "grpc+stdio://"):
		return cmdGRPCStdio(format, uri, args)
	case strings.HasPrefix(uri, "grpc+unix://"):
		return cmdGRPCDirect(format, "unix://"+strings.TrimPrefix(uri, "grpc+unix://"), args)
	case strings.HasPrefix(uri, "grpc+ws://") || strings.HasPrefix(uri, "grpc+wss://"):
		return cmdGRPCWebSocket(format, uri, args)
	default:
		return cmdGRPCTCP(format, uri, args)
	}
}

// cmdGRPCTCP handles grpc://host:port and grpc://holon (ephemeral TCP).
func cmdGRPCTCP(format Format, uri string, args []string) int {
	address := strings.TrimPrefix(uri, "grpc://")

	_, _, err := net.SplitHostPort(address)
	isHostPort := err == nil

	if isHostPort {
		return cmdGRPCDirect(format, address, args)
	}

	// Ephemeral TCP mode: address is a holon name
	holonName := address
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "op grpc: method required for ephemeral mode")
		fmt.Fprintf(os.Stderr, "usage: op grpc://%s <method>\n", holonName)
		return 1
	}

	scheme, err := selectTransport(holonName)
	if err == nil {
		switch scheme {
		case "mem":
			return cmdGRPCMem(format, holonName, args)
		case "stdio":
			return cmdGRPCStdio(format, "grpc+stdio://"+holonName, args)
		}
	}

	binary, err := resolveHolon(holonName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op grpc: holon %q not found\n", holonName)
		return 1
	}

	// Pick an ephemeral port via SDK transport
	lis, err := transport.Listen("tcp://:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "op grpc: cannot allocate port: %v\n", err)
		return 1
	}
	port := fmt.Sprintf("%d", lis.Addr().(*net.TCPAddr).Port)
	lis.Close()

	cmd := exec.Command(binary, "serve", "--listen", "tcp://:"+port)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "op grpc: cannot start %s: %v\n", holonName, err)
		return 1
	}
	defer func() {
		cmd.Process.Kill() //nolint:errcheck
		cmd.Wait()         //nolint:errcheck
	}()

	target := fmt.Sprintf("localhost:%s", port)
	ready := false
	for i := 0; i < 50; i++ {
		conn, err := net.DialTimeout("tcp", target, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		fmt.Fprintf(os.Stderr, "op grpc: %s did not start within 5s on port %s\n", holonName, port)
		return 1
	}

	return cmdGRPCDirect(format, target, args)
}

// cmdGRPCStdio handles grpc+stdio://holon — launches the holon with
// serve --listen stdio:// and communicates via stdin/stdout pipes.
func cmdGRPCStdio(format Format, uri string, args []string) int {
	holonName := strings.TrimPrefix(uri, "grpc+stdio://")
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "op grpc: method required")
		fmt.Fprintf(os.Stderr, "usage: op grpc+stdio://%s <method>\n", holonName)
		return 1
	}

	binary, err := resolveHolon(holonName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op grpc: holon %q not found\n", holonName)
		return 1
	}

	method := args[0]
	inputJSON := []byte("{}")
	if len(args) > 1 {
		inputJSON = []byte(args[1])
	}

	result, err := callViaStdio(binary, method, inputJSON)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op grpc: %v\n", err)
		return 1
	}

	fmt.Println(formatRPCOutput(format, method, result))
	return 0
}

// cmdGRPCWebSocket handles grpc+ws://host:port[/path] and grpc+wss://...
// Connects to an existing WebSocket gRPC server.
func cmdGRPCWebSocket(format Format, uri string, args []string) int {
	// Convert grpc+ws://host:port → ws://host:port
	// Convert grpc+wss://host:port → wss://host:port
	wsURI := strings.TrimPrefix(uri, "grpc+")

	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "op grpc: method required")
		fmt.Fprintf(os.Stderr, "usage: op %s <method>\n", uri)
		return 1
	}

	method := args[0]
	inputJSON := "{}"
	if len(args) > 1 {
		inputJSON = args[1]
	}

	// Ensure path includes /grpc if not specified
	if !strings.Contains(wsURI[5:], "/") { // skip "ws://" prefix
		wsURI += "/grpc"
	}

	result, err := grpcclient.DialWebSocket(wsURI, method, inputJSON)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op grpc: %v\n", err)
		return 1
	}

	fmt.Println(formatRPCOutput(format, method, []byte(result.Output)))
	return 0
}

// cmdGRPCDirect calls an RPC on an existing gRPC server at the given address.
func cmdGRPCDirect(format Format, address string, args []string) int {
	if len(args) == 0 {
		methods, err := grpcclient.ListMethods(address)
		if err != nil {
			fmt.Fprintf(os.Stderr, "op grpc: %v\n", err)
			return 1
		}
		fmt.Printf("Available methods at %s:\n", address)
		for _, m := range methods {
			fmt.Printf("  %s\n", m)
		}
		return 0
	}

	method := args[0]
	inputJSON := "{}"
	if len(args) > 1 {
		inputJSON = args[1]
	}

	result, err := grpcclient.Dial(address, method, inputJSON)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op grpc: %v\n", err)
		return 1
	}

	fmt.Println(formatRPCOutput(format, method, []byte(result.Output)))
	return 0
}

func discoverInPath() []string {
	known := []string{"who", "atlas", "translate", "op"}
	var found []string
	for _, name := range known {
		if p, err := exec.LookPath(name); err == nil {
			found = append(found, fmt.Sprintf("%-12s → %s", name, p))
		}
	}
	return found
}

// --- Namespace dispatch ---

// cmdHolon runs `op <holon> <command> [args...]` through the transport chain.
func cmdHolon(format Format, holon string, args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "op: missing command for holon %q\n", holon)
		return 1
	}

	method, inputJSON, err := mapHolonCommandToRPC(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op: %v\n", err)
		return 1
	}

	scheme, err := selectTransport(holon)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op: %v\n", err)
		return 1
	}

	switch scheme {
	case "mem":
		output, err := callViaMem(holon, method, inputJSON)
		if err != nil {
			fmt.Fprintf(os.Stderr, "op: %v\n", err)
			return 1
		}
		fmt.Println(formatRPCOutput(format, method, []byte(output)))
		return 0
	case "stdio":
		binary, err := resolveHolon(holon)
		if err != nil {
			fmt.Fprintf(os.Stderr, "op: unknown holon %q\n", holon)
			return 1
		}
		output, err := callViaStdio(binary, method, []byte(inputJSON))
		if err != nil {
			fmt.Fprintf(os.Stderr, "op: %v\n", err)
			return 1
		}
		fmt.Println(formatRPCOutput(format, method, output))
		return 0
	default:
		return cmdGRPCTCP(format, "grpc://"+holon, []string{method, inputJSON})
	}
}

func mapHolonCommandToRPC(args []string) (method string, inputJSON string, err error) {
	command := strings.TrimSpace(args[0])
	rest := args[1:]

	method = mapCommandNameToMethod(command)
	if len(rest) > 0 && looksLikeJSON(rest[0]) {
		return method, rest[0], nil
	}

	switch strings.ToLower(command) {
	case "list":
		if len(rest) > 0 {
			payload, err := json.Marshal(map[string]string{"rootDir": rest[0]})
			if err != nil {
				return "", "", err
			}
			return method, string(payload), nil
		}
		return method, "{}", nil
	case "show":
		if len(rest) < 1 {
			return "", "", fmt.Errorf("show requires <uuid>")
		}
		payload, err := json.Marshal(map[string]string{"uuid": rest[0]})
		if err != nil {
			return "", "", err
		}
		return method, string(payload), nil
	case "pin":
		if len(rest) < 1 {
			return "", "", fmt.Errorf("pin requires <uuid>")
		}
		payload, err := json.Marshal(map[string]string{"uuid": rest[0]})
		if err != nil {
			return "", "", err
		}
		return method, string(payload), nil
	default:
		return method, "{}", nil
	}
}

func mapCommandNameToMethod(command string) string {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "new":
		return "CreateIdentity"
	case "list":
		return "ListIdentities"
	case "show":
		return "ShowIdentity"
	case "pin":
		return "PinVersion"
	default:
		return command
	}
}

func looksLikeJSON(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}

// cmdDispatch runs `op <holon> <command> [args...]` by finding the
// holon binary and executing it as a subprocess.
func cmdDispatch(holon string, args []string) int {
	// Try to find the holon binary by alias
	binary, err := resolveHolon(holon)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op: unknown holon %q\n", holon)
		fmt.Fprintln(os.Stderr, "Run 'op discover' to see available holons.")
		return 1
	}

	cmd := exec.Command(binary, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "op: %v\n", err)
		return 1
	}
	return 0
}

// resolveHolon finds a holon binary by alias name. Search order:
// 1. holons/<name>/<name> (sibling submodule binary)
// 2. $PATH
func resolveHolon(name string) (string, error) {
	// Alias mapping: "who" → "sophia-who", etc.
	aliases := map[string]string{
		"who":       "who",
		"atlas":     "atlas",
		"translate": "translate",
	}

	binName := name
	if mapped, ok := aliases[name]; ok {
		binName = mapped
	}

	// Try sibling holon directories
	candidates := []string{
		filepath.Join("holons", name, binName),
		filepath.Join("holons", "sophia-"+name, binName),
		filepath.Join("holons", "rhizome-"+name, binName),
		filepath.Join("holons", "babel-fish-"+name, binName),
	}

	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}

	// Try $PATH
	if p, err := exec.LookPath(binName); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("holon %q not found", name)
}

// --- Flag helpers ---

// flagValue extracts --key value from args. Returns "" if not found.
func flagValue(args []string, key string) string {
	for i, a := range args {
		if a == key && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// flagOrDefault returns the flag value if present, else the default.
func flagOrDefault(args []string, key, defaultVal string) string {
	if v := flagValue(args, key); v != "" {
		return v
	}
	return defaultVal
}

func parseGlobalFormat(args []string) (Format, []string, error) {
	format := FormatText
	i := 0
	for i < len(args) {
		switch {
		case args[i] == "--format" || args[i] == "-f":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("%s requires a value (text or json)", args[i])
			}
			parsed, err := parseFormat(args[i+1])
			if err != nil {
				return "", nil, err
			}
			format = parsed
			i += 2
		case strings.HasPrefix(args[i], "--format="):
			parsed, err := parseFormat(strings.TrimPrefix(args[i], "--format="))
			if err != nil {
				return "", nil, err
			}
			format = parsed
			i++
		case strings.HasPrefix(args[i], "-f="):
			parsed, err := parseFormat(strings.TrimPrefix(args[i], "-f="))
			if err != nil {
				return "", nil, err
			}
			format = parsed
			i++
		default:
			return format, args[i:], nil
		}
	}
	return format, nil, nil
}

func parseFormat(value string) (Format, error) {
	switch Format(strings.ToLower(strings.TrimSpace(value))) {
	case FormatText:
		return FormatText, nil
	case FormatJSON:
		return FormatJSON, nil
	default:
		return "", fmt.Errorf("invalid --format %q (supported: text, json)", value)
	}
}
