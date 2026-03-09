// Package cli implements OP's command routing — transport-chain dispatch,
// URI dispatch, and OP's own commands.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/organic-programming/go-holons/pkg/transport"
	"github.com/organic-programming/grace-op/internal/grpcclient"
	"github.com/organic-programming/grace-op/internal/holons"
	"github.com/organic-programming/grace-op/internal/server"
)

// Run dispatches the command and returns an exit code.
func Run(args []string, version string) int {
	format, quiet, args, err := parseGlobalOptions(args)
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
	case "check":
		return cmdLifecycle(format, quiet, holons.OperationCheck, rest)
	case "build":
		return cmdLifecycle(format, quiet, holons.OperationBuild, rest)
	case "test":
		return cmdLifecycle(format, quiet, holons.OperationTest, rest)
	case "clean":
		return cmdLifecycle(format, quiet, holons.OperationClean, rest)
	case "install":
		return cmdInstall(format, quiet, rest)
	case "uninstall":
		return cmdUninstall(format, quiet, rest)
	case "mod":
		return cmdMod(format, quiet, rest)
	case "run":
		return cmdRun(format, quiet, rest)
	case "discover":
		return cmdDiscover(format)
	case "inspect":
		return cmdInspect(format, rest)
	case "mcp":
		return cmdMCP(rest, version)
	case "tools":
		return cmdTools(format, rest)
	case "env":
		return cmdEnv(format, rest)
	case "serve":
		return cmdServe(rest)
	case "version":
		fmt.Printf("op %s\n", version)
		return 0
	case "completion":
		return cmdCompletion(rest)
	case "__complete":
		return cmdComplete(rest)
	case "help", "--help", "-h":
		PrintUsage()
		return 0
	case "new", "list", "show":
		return cmdWho(format, quiet, cmd, rest)

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
  -q, --quiet                           suppress progress and suggestions

Holon dispatch (transport chain):
  op <holon> <command> [args]            dispatch via mem://, stdio://, or tcp://

Direct gRPC URI dispatch:
  op grpc://<host:port> <method>         gRPC over TCP (existing server)
  op grpc+stdio://<holon> <method>       gRPC over stdio pipe (ephemeral)
  op grpc+unix://<path> <method>         gRPC over Unix socket
  op grpc+ws://<host:port> <method>      gRPC over WebSocket
  op grpc+wss://<host:port> <method>     gRPC over secure WebSocket
  op run <holon> [flags]                 build if needed, then launch in foreground
  op run <holon>:<port>                  shorthand for --listen tcp://:<port>

OP commands:
  op list [root]                         list local + cached holons natively
  op show <uuid-or-prefix>               display a holon identity natively
  op new [--json <payload>]              create a holon identity natively
  op inspect <slug|host:port> [--json]   inspect a holon's API offline or via Describe
  op mcp <slug> [slug2...]               start an MCP server for one or more holons
  op tools <slug> [--format <fmt>]       output tool definitions (openai, anthropic, mcp)
  op check [<holon-or-path>]             validate holon.yaml and prerequisites
  op build [<holon-or-path>] [flags]     build a holon artifact via its runner
  op test [<holon-or-path>]              run a holon's test contract
  op clean [<holon-or-path>]             remove .op/ build outputs
  op install [<holon-or-path>]           install a built artifact into $OPBIN
  op uninstall <holon>                   remove an installed artifact from $OPBIN
  op mod <command>                       manage holon.mod and holon.sum
  op env [--init] [--shell]              print resolved OPPATH / OPBIN / ROOT

Build flags:
  --target <macos|linux|windows|ios|ios-simulator|tvos|tvos-simulator|watchos|watchos-simulator|visionos|visionos-simulator|android|all>   platform target (default: current OS)
  --mode <debug|release|profile>               build mode (default: debug)
  --dry-run                                    print resolved plan, do not execute

Run flags:
  --listen <URI>                               listen address for service holons (default: stdio://)
  --no-build                                   fail if the artifact is missing instead of building
  --target <...>                               pass build target through if a build is needed
  --mode <debug|release|profile>               pass build mode through if a build is needed

  op discover                            list available holons
  op serve [--listen tcp://:9090]        start OP's own gRPC server
  op version                             show op version
  op help                                this message
`)
}

// --- OP's own commands ---

type discoverEntry struct {
	Slug         string `json:"slug"`
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
	Entries           []discoverEntry `json:"entries"`
	InstalledBinaries []string        `json:"installed_binaries,omitempty"`
	PathBinaries      []string        `json:"path_binaries"`
}

func cmdDiscover(format Format) int {
	located, err := holons.DiscoverLocalHolons()
	if err != nil {
		fmt.Fprintf(os.Stderr, "op discover: %v\n", err)
		return 1
	}
	cached, err := holons.DiscoverCachedHolons()
	if err != nil {
		fmt.Fprintf(os.Stderr, "op discover: %v\n", err)
		return 1
	}

	entries := make([]discoverEntry, 0, len(located)+len(cached))
	for _, h := range append(append([]holons.LocalHolon{}, located...), cached...) {
		slug := h.Identity.Slug()
		if slug == "" {
			slug = filepath.Base(h.Dir)
		}
		entries = append(entries, discoverEntry{
			Slug:         slug,
			UUID:         h.Identity.UUID,
			GivenName:    h.Identity.GivenName,
			FamilyName:   h.Identity.FamilyName,
			Lang:         h.Identity.Lang,
			Clade:        h.Identity.Clade,
			Status:       h.Identity.Status,
			RelativePath: h.RelativePath,
			Origin:       discoverOrigin(h.Origin),
		})
	}
	installedHolons := holons.DiscoverInOPBIN()
	pathHolons := discoverInPath()

	if format == FormatJSON {
		payload := discoverOutput{
			Entries:           entries,
			InstalledBinaries: installedHolons,
			PathBinaries:      pathHolons,
		}
		out, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "op discover: %v\n", err)
			return 1
		}
		fmt.Println(string(out))
		return 0
	}

	printDiscoverTable(entries, installedHolons, pathHolons)
	return 0
}

func printDiscoverTable(entries []discoverEntry, installedHolons, pathHolons []string) {
	if len(entries) == 0 {
		fmt.Println("No holons found in known roots.")
	} else {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SLUG\tNAME\tLANG\tCLADE\tSTATUS\tORIGIN\tUUID")
		for _, entry := range entries {
			fmt.Fprintf(
				w,
				"%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				defaultDash(entry.Slug),
				discoverDisplayName(entry),
				defaultDash(entry.Lang),
				defaultDash(entry.Clade),
				defaultDash(entry.Status),
				defaultDash(entry.Origin),
				defaultDash(entry.UUID),
			)
		}
		_ = w.Flush()
	}

	if len(installedHolons) > 0 {
		fmt.Println("\nIn $OPBIN:")
		for _, name := range installedHolons {
			fmt.Printf("  %s\n", name)
		}
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

func discoverOrigin(origin string) string {
	if strings.TrimSpace(origin) == "" {
		return "local"
	}
	return origin
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

type runOptions struct {
	ListenURI      string
	ListenExplicit bool
	NoBuild        bool
	Target         string
	Mode           string
}

// cmdRun builds a holon artifact if needed, then launches it in the foreground.
func cmdRun(format Format, globalQuiet bool, args []string) int {
	ui, args, _ := extractQuietFlag(args)
	quiet := globalQuiet || ui.Quiet

	holonName, opts, err := parseRunArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op run: %v\n", err)
		return 1
	}
	printer := commandProgress(format, quiet)

	printer.Step("resolving " + holonName + "...")

	if binary := resolveInstalledBinary(holonName); binary != "" {
		printer.Step("launching " + holonName + "...")
		cmd := exec.Command(binary, "serve", "--listen", opts.ListenURI)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := runForeground(cmd); err != nil {
			if code, ok := commandExitCode(err); ok {
				return code
			}
			printer.Done("run failed", err)
			fmt.Fprintf(os.Stderr, "op run: %v\n", err)
			return 1
		}
		printer.Done(fmt.Sprintf("%s exited in %s", holonName, humanElapsed(printer)), nil)
		return 0
	}

	target, err := holons.ResolveTarget(holonName)
	if err != nil {
		printer.Done("run failed", err)
		fmt.Fprintf(os.Stderr, "op run: %v\n", err)
		return 1
	}
	if target.ManifestErr != nil {
		printer.Done("run failed", target.ManifestErr)
		fmt.Fprintf(os.Stderr, "op run: %v\n", target.ManifestErr)
		return 1
	}
	if target.Manifest == nil {
		err := fmt.Errorf("no %s found in %s", holons.ManifestFileName, target.RelativePath)
		printer.Done("run failed", err)
		fmt.Fprintf(os.Stderr, "op run: %v\n", err)
		return 1
	}

	ctx, err := holons.ResolveBuildContext(target.Manifest, holons.BuildOptions{
		Target: opts.Target,
		Mode:   opts.Mode,
	})
	if err != nil {
		printer.Done("run failed", err)
		fmt.Fprintf(os.Stderr, "op run: %v\n", err)
		return 1
	}
	if ctx.Target == "all" {
		err := fmt.Errorf("target %q cannot be launched", ctx.Target)
		printer.Done("run failed", err)
		fmt.Fprintf(os.Stderr, "op run: %v\n", err)
		return 1
	}

	isComposite := target.Manifest.Manifest.Kind == holons.KindComposite
	if isComposite && opts.ListenExplicit {
		err := fmt.Errorf("--listen is only supported for service holons")
		printer.Done("run failed", err)
		fmt.Fprintf(os.Stderr, "op run: %v\n", err)
		return 1
	}

	artifactPath := target.Manifest.ArtifactPath(ctx)
	if artifactPath == "" {
		err := fmt.Errorf("no artifact declared for target %q mode %q", ctx.Target, ctx.Mode)
		printer.Done("run failed", err)
		fmt.Fprintf(os.Stderr, "op run: %v\n", err)
		return 1
	}
	if _, err := os.Stat(artifactPath); err != nil {
		if !os.IsNotExist(err) {
			printer.Done("run failed", err)
			fmt.Fprintf(os.Stderr, "op run: %v\n", err)
			return 1
		}
		if opts.NoBuild {
			err := fmt.Errorf("artifact missing: %s", artifactPath)
			printer.Done("run failed", err)
			fmt.Fprintf(os.Stderr, "op run: %v\n", err)
			return 1
		}
		printer.Step("building " + holonName + "...")
		if _, err := holons.ExecuteLifecycle(holons.OperationBuild, holonName, holons.BuildOptions{
			Target:   opts.Target,
			Mode:     opts.Mode,
			Progress: printer,
		}); err != nil {
			printer.Done("run failed", err)
			fmt.Fprintf(os.Stderr, "op run: %v\n", err)
			return 1
		}
		if _, err := os.Stat(artifactPath); err != nil {
			if os.IsNotExist(err) {
				err = fmt.Errorf("artifact missing: %s", artifactPath)
			}
			printer.Done("run failed", err)
			fmt.Fprintf(os.Stderr, "op run: %v\n", err)
			return 1
		}
	}

	cmd, err := commandForArtifact(target.Manifest, ctx, opts.ListenURI)
	if err != nil {
		printer.Done("run failed", err)
		fmt.Fprintf(os.Stderr, "op run: %v\n", err)
		return 1
	}
	printer.Step("launching " + holonName + "...")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := runForeground(cmd); err != nil {
		if code, ok := commandExitCode(err); ok {
			return code
		}
		printer.Done("run failed", err)
		fmt.Fprintf(os.Stderr, "op run: %v\n", err)
		return 1
	}
	printer.Done(fmt.Sprintf("%s exited in %s", holonName, humanElapsed(printer)), nil)
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
	return holons.DiscoverInPath()
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
	// Try to find the holon binary by selector.
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

// resolveHolon finds a holon binary by selector.
func resolveHolon(name string) (string, error) {
	return holons.ResolveBinary(name)
}

func resolveInstalledBinary(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || strings.ContainsAny(trimmed, `/\`) {
		return ""
	}
	return holons.ResolveInstalledBinary(trimmed)
}

func parseRunArgs(args []string) (string, runOptions, error) {
	opts := runOptions{ListenURI: "stdio://"}
	var positional []string

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--listen":
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("--listen requires a value")
			}
			opts.ListenURI = args[i+1]
			opts.ListenExplicit = true
			i++
		case args[i] == "--no-build":
			opts.NoBuild = true
		case args[i] == "--target":
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("--target requires a value")
			}
			opts.Target = args[i+1]
			i++
		case args[i] == "--mode":
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("--mode requires a value")
			}
			opts.Mode = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--"):
			return "", opts, fmt.Errorf("unknown flag %q", args[i])
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) == 0 {
		return "", opts, fmt.Errorf("requires <holon> [flags]")
	}
	if len(positional) > 1 {
		return "", opts, fmt.Errorf("accepts exactly one <holon>")
	}

	holonName := strings.TrimSpace(positional[0])
	if legacyName, legacyListen, ok := parseLegacyRunTarget(holonName); ok {
		if opts.ListenExplicit {
			return "", opts, fmt.Errorf("cannot combine --listen with <holon>:<port> shorthand")
		}
		holonName = legacyName
		opts.ListenURI = legacyListen
		opts.ListenExplicit = true
	}
	if holonName == "" {
		return "", opts, fmt.Errorf("requires <holon> [flags]")
	}

	return holonName, opts, nil
}

func parseLegacyRunTarget(value string) (string, string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.ContainsAny(trimmed, `/\`) {
		return "", "", false
	}
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), "tcp://:" + strings.TrimSpace(parts[1]), true
}

func commandForArtifact(manifest *holons.LoadedManifest, ctx holons.BuildContext, listenURI string) (*exec.Cmd, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest required")
	}
	if manifest.Manifest.Kind == holons.KindComposite {
		artifactPath := manifest.ArtifactPath(ctx)
		info, err := os.Stat(artifactPath)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			if isMacAppBundle(artifactPath) && runtime.GOOS == "darwin" {
				return exec.Command("open", "-W", artifactPath), nil
			}
			return nil, fmt.Errorf("artifact is not directly launchable: %s", artifactPath)
		}
		return exec.Command(artifactPath), nil
	}

	binaryPath := manifest.BinaryPath()
	if strings.TrimSpace(binaryPath) == "" {
		return nil, fmt.Errorf("no binary declared for %s", manifest.Name)
	}
	return exec.Command(binaryPath, "serve", "--listen", listenURI), nil
}

func isMacAppBundle(path string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), ".app")
}

func runForeground(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	signals := []os.Signal{os.Interrupt}
	if runtime.GOOS != "windows" {
		signals = append(signals, syscall.SIGTERM)
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, signals...)
	defer signal.Stop(sigCh)

	for {
		select {
		case err := <-waitCh:
			return err
		case sig := <-sigCh:
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		}
	}
}

func commandExitCode(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
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

func parseGlobalOptions(args []string) (Format, bool, []string, error) {
	format := FormatText
	quiet := false
	i := 0
	for i < len(args) {
		switch {
		case args[i] == "--quiet" || args[i] == "-q":
			quiet = true
			i++
		case args[i] == "--format" || args[i] == "-f":
			if i+1 >= len(args) {
				return "", false, nil, fmt.Errorf("%s requires a value (text or json)", args[i])
			}
			parsed, err := parseFormat(args[i+1])
			if err != nil {
				return "", false, nil, err
			}
			format = parsed
			i += 2
		case strings.HasPrefix(args[i], "--format="):
			parsed, err := parseFormat(strings.TrimPrefix(args[i], "--format="))
			if err != nil {
				return "", false, nil, err
			}
			format = parsed
			i++
		case strings.HasPrefix(args[i], "-f="):
			parsed, err := parseFormat(strings.TrimPrefix(args[i], "-f="))
			if err != nil {
				return "", false, nil, err
			}
			format = parsed
			i++
		default:
			return format, quiet, args[i:], nil
		}
	}
	return format, quiet, nil, nil
}

func parseGlobalFormat(args []string) (Format, []string, error) {
	format, _, remaining, err := parseGlobalOptions(args)
	return format, remaining, err
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
