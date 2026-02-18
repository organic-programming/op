// Package cli implements OP's command routing — promoted verbs,
// namespace dispatch, and OP's own commands.
package cli

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/organic-programming/grace-op/internal/grpcclient"
	"github.com/organic-programming/grace-op/internal/server"
	"github.com/organic-programming/sophia-who/pkg/identity"
)

// Run dispatches the command and returns an exit code.
func Run(args []string, version string) int {
	cmd := args[0]
	rest := args[1:]

	switch cmd {
	// --- Promoted verbs (delegate to Sophia's API) ---
	case "new":
		return cmdNew(rest)
	case "list":
		return cmdList(rest)
	case "show":
		return cmdShow(rest)
	case "pin":
		return cmdPin(rest)

	// --- OP's own commands ---
	case "run":
		return cmdRun(rest)
	case "discover":
		return cmdDiscover()
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
			return cmdGRPC(cmd, rest)
		}
		return cmdDispatch(cmd, rest)
	}
}

// PrintUsage displays the help text.
func PrintUsage() {
	fmt.Print(`op — the Organic Programming CLI

Promoted verbs (Sophia Who?):
  op new                                 create a new holon identity
  op list [root]                         list all known holons in root
  op show <uuid>                         display a holon's identity
  op pin <uuid>                          capture version/commit/arch

Facet dispatch:
  op <holon> <command> [args]            CLI facet (subprocess)
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

// --- Promoted verbs (use Sophia's pkg/identity directly) ---

func cmdNew(args []string) int {
	id := identity.New()

	// Interactive-style: read from flags or prompt
	id.GivenName = flagOrPrompt(args, "--name", "Given name: ")
	id.FamilyName = flagOrPrompt(args, "--family", "Family name: ")
	id.Motto = flagOrPrompt(args, "--motto", "Motto: ")
	id.Composer = flagOrPrompt(args, "--composer", "Composer: ")
	id.Clade = flagOrDefault(args, "--clade", "deterministic/pure")
	id.Lang = flagOrDefault(args, "--lang", "go")

	// Build output directory name from the holon name
	dirName := strings.ToLower(id.GivenName + "-" + strings.TrimSuffix(id.FamilyName, "?"))
	dirName = strings.ReplaceAll(dirName, " ", "-")
	outputDir := flagOrDefault(args, "--output", dirName)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "op: cannot create directory: %v\n", err)
		return 1
	}

	outputPath := filepath.Join(outputDir, "HOLON.md")
	if err := identity.WriteHolonMD(id, outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "op: %v\n", err)
		return 1
	}

	fmt.Printf("Created %s\n", outputPath)
	fmt.Printf("  UUID:  %s\n", id.UUID)
	fmt.Printf("  Name:  %s %s\n", id.GivenName, id.FamilyName)
	fmt.Printf("  Motto: %s\n", id.Motto)
	return 0
}

func cmdList(args []string) int {
	root, err := parseListRoot(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op list: %v\n", err)
		return 1
	}

	entries, err := listHolons(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op list: %v\n", err)
		return 1
	}

	if len(entries) == 0 {
		fmt.Println("No holons found.")
		return 0
	}

	fmt.Printf("%-38s %-33s %-8s %-25s %-8s %s\n", "UUID", "NAME", "ORIGIN", "CLADE", "STATUS", "PATH")
	fmt.Println(strings.Repeat("─", 150))

	for _, e := range entries {
		name := e.id.GivenName
		if e.id.FamilyName != "" {
			name += " " + e.id.FamilyName
		}
		fmt.Printf("%-38s %-33s %-8s %-25s %-8s %s\n", e.id.UUID, name, e.origin, e.id.Clade, e.id.Status, e.path)
	}

	return 0
}

type listedHolon struct {
	id     identity.Identity
	origin string
	path   string
}

func parseListRoot(args []string) (string, error) {
	switch len(args) {
	case 0:
		return ".", nil
	case 1:
		return args[0], nil
	default:
		return "", fmt.Errorf("usage: op list [root]")
	}
}

func listHolons(root string) ([]listedHolon, error) {
	root = filepath.Clean(root)
	var entries []listedHolon
	seen := map[string]struct{}{}

	add := func(scanRoot, origin string, strict bool) error {
		holons, err := identity.FindAllWithPaths(scanRoot)
		if err != nil {
			if strict {
				return err
			}
			return nil
		}
		for _, h := range holons {
			key := h.Identity.UUID
			if key == "" {
				key = h.Path
			}
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			entries = append(entries, listedHolon{
				id:     h.Identity,
				origin: origin,
				path:   relHolonDir(root, h.Path),
			})
			seen[key] = struct{}{}
		}
		return nil
	}

	// Prioritize conventional local holons folder if present.
	if err := add(filepath.Join(root, "holons"), "local", false); err != nil {
		return nil, err
	}
	// Then scan root recursively (standalone projects, examples, etc.).
	if err := add(root, "local", true); err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].path < entries[j].path
	})

	return entries, nil
}

func relHolonDir(root, holonPath string) string {
	dir := filepath.Dir(holonPath)
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	return filepath.Clean(rel)
}

func cmdShow(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "op show: UUID required")
		return 1
	}

	path, err := identity.FindByUUID(".", args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "op show: %v\n", err)
		return 1
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op show: %v\n", err)
		return 1
	}

	id, _, err := identity.ParseFrontmatter(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op show: %v\n", err)
		return 1
	}

	fmt.Printf("UUID:          %s\n", id.UUID)
	fmt.Printf("Name:          %s %s\n", id.GivenName, id.FamilyName)
	fmt.Printf("Motto:         %s\n", id.Motto)
	fmt.Printf("Composer:      %s\n", id.Composer)
	fmt.Printf("Clade:         %s\n", id.Clade)
	fmt.Printf("Status:        %s\n", id.Status)
	fmt.Printf("Born:          %s\n", id.Born)
	fmt.Printf("Lang:          %s\n", id.Lang)
	fmt.Printf("File:          %s\n", path)
	if id.BinaryVersion != "" {
		fmt.Printf("Version:       %s\n", id.BinaryVersion)
	}
	if id.GitTag != "" {
		fmt.Printf("Git tag:       %s\n", id.GitTag)
	}
	if id.GitCommit != "" {
		fmt.Printf("Git commit:    %s\n", id.GitCommit)
	}
	if len(id.Parents) > 0 {
		fmt.Printf("Parents:       %s\n", strings.Join(id.Parents, ", "))
	}
	return 0
}

func cmdPin(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "op pin: UUID required")
		return 1
	}

	path, err := identity.FindByUUID(".", args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "op pin: %v\n", err)
		return 1
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op pin: %v\n", err)
		return 1
	}

	id, _, err := identity.ParseFrontmatter(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op pin: %v\n", err)
		return 1
	}

	// Update pin fields from flags
	if v := flagValue(args[1:], "--version"); v != "" {
		id.BinaryVersion = v
	}
	if v := flagValue(args[1:], "--tag"); v != "" {
		id.GitTag = v
	}
	if v := flagValue(args[1:], "--commit"); v != "" {
		id.GitCommit = v
	}
	if v := flagValue(args[1:], "--os"); v != "" {
		id.OS = v
	}
	if v := flagValue(args[1:], "--arch"); v != "" {
		id.Arch = v
	}

	if err := identity.WriteHolonMD(id, path); err != nil {
		fmt.Fprintf(os.Stderr, "op pin: %v\n", err)
		return 1
	}

	fmt.Printf("Pinned %s %s → %s\n", id.GivenName, id.FamilyName, path)
	return 0
}

// --- OP's own commands ---

func cmdDiscover() int {
	holons, err := identity.FindAll(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "op discover: %v\n", err)
		return 1
	}

	fmt.Println("Local holons:")
	for _, h := range holons {
		name := h.GivenName
		if h.FamilyName != "" {
			name += " " + h.FamilyName
		}
		fmt.Printf("  %-20s %s  [%s]\n", name, h.UUID[:8], h.Clade)
	}

	// Check $PATH for holon binaries
	pathHolons := discoverInPath()
	if len(pathHolons) > 0 {
		fmt.Println("\nIn $PATH:")
		for _, name := range pathHolons {
			fmt.Printf("  %s\n", name)
		}
	}

	return 0
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
func cmdGRPC(uri string, args []string) int {
	switch {
	case strings.HasPrefix(uri, "grpc+stdio://"):
		return cmdGRPCStdio(uri, args)
	case strings.HasPrefix(uri, "grpc+unix://"):
		return cmdGRPCDirect("unix://"+strings.TrimPrefix(uri, "grpc+unix://"), args)
	case strings.HasPrefix(uri, "grpc+ws://") || strings.HasPrefix(uri, "grpc+wss://"):
		return cmdGRPCWebSocket(uri, args)
	default:
		return cmdGRPCTCP(uri, args)
	}
}

// cmdGRPCTCP handles grpc://host:port and grpc://holon (ephemeral TCP).
func cmdGRPCTCP(uri string, args []string) int {
	address := strings.TrimPrefix(uri, "grpc://")

	_, _, err := net.SplitHostPort(address)
	isHostPort := err == nil

	if isHostPort {
		return cmdGRPCDirect(address, args)
	}

	// Ephemeral TCP mode: address is a holon name
	holonName := address
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "op grpc: method required for ephemeral mode")
		fmt.Fprintf(os.Stderr, "usage: op grpc://%s <method>\n", holonName)
		return 1
	}

	binary, err := resolveHolon(holonName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op grpc: holon %q not found\n", holonName)
		return 1
	}

	// Pick an ephemeral port
	lis, err := net.Listen("tcp", ":0")
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

	return cmdGRPCDirect(target, args)
}

// cmdGRPCStdio handles grpc+stdio://holon — launches the holon with
// serve --listen stdio:// and communicates via stdin/stdout pipes.
func cmdGRPCStdio(uri string, args []string) int {
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
	inputJSON := "{}"
	if len(args) > 1 {
		inputJSON = args[1]
	}

	result, err := grpcclient.DialStdio(binary, method, inputJSON)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op grpc: %v\n", err)
		return 1
	}

	fmt.Println(result.Output)
	return 0
}

// cmdGRPCWebSocket handles grpc+ws://host:port[/path] and grpc+wss://...
// Connects to an existing WebSocket gRPC server.
func cmdGRPCWebSocket(uri string, args []string) int {
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

	fmt.Println(result.Output)
	return 0
}

// cmdGRPCDirect calls an RPC on an existing gRPC server at the given address.
func cmdGRPCDirect(address string, args []string) int {
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

	fmt.Println(result.Output)
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

// flagOrPrompt returns the flag value if present, else prompts the user.
func flagOrPrompt(args []string, key, prompt string) string {
	if v := flagValue(args, key); v != "" {
		return v
	}
	fmt.Print(prompt)
	var input string
	fmt.Scanln(&input)
	return input
}
