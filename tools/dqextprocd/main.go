// Command dqextprocd is C7b: the gRPC service Envoy calls on the request path. It loads one
// artefact tree, compiles the plans, and serves ext_proc on an h2c listener.
//
// It is deliberately dumb: no config watching, no hot reload, no registry client. A rule change
// reaches it the way every other enforcement change does — a new pod pinned to a new digest —
// because S4 says the runtime never invents rules and a filter that reloads its own bundle is a
// filter that can enforce something nobody published.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"

	"github.com/dennisholee/dq-guardrail/engine"
	"github.com/dennisholee/dq-guardrail/engine/adapter/extproc"
)

func main() {
	root := flag.String("root", ".", "artefact tree")
	addr := flag.String("addr", ":8081", "gRPC listen address (h2c, no TLS)")
	modeName := flag.String("mode", "ENFORCE", "OFF, SHADOW, WARN or ENFORCE")
	maxBody := flag.Int64("max-body", 1<<20, "largest request body to evaluate, in bytes")
	flag.Parse()

	mode, err := parseMode(*modeName)
	if err != nil {
		fatal(err)
	}

	m, err := engine.Load(*root)
	if err != nil {
		fatal(fmt.Errorf("load %s: %w", *root, err))
	}
	e, err := engine.NewEngineWithClock(m, time.Now)
	if err != nil {
		fatal(fmt.Errorf("engine: %w", err))
	}

	// An HTTP plan is addressed by {intent}-{object}, which is the route segment the OpenAPI
	// declares and the key Envoy sends in :path.
	plans := map[string]*engine.Plan{}
	for _, p := range m.Plans {
		if p.Channel != "http" {
			continue
		}
		plans["/"+p.Intent+"-"+p.Object] = p
	}

	srv, err := dqextproc.NewServer(dqextproc.Config{
		Engine:  e,
		Plans:   plans,
		Mode:    mode,
		MaxBody: *maxBody,
	})
	if err != nil {
		fatal(err)
	}

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		fatal(fmt.Errorf("listen %s: %w", *addr, err))
	}

	fmt.Fprintf(os.Stderr, "dqextprocd: %s, mode %s, %d http plan(s)\n", *addr, mode, len(plans))
	for path := range plans {
		fmt.Fprintf(os.Stderr, "  deciding %s\n", path)
	}

	g := grpc.NewServer()
	srv.Register(g)
	if err := g.Serve(lis); err != nil {
		fatal(err)
	}
}

// parseMode accepts the four modes spelled as the binding spells them.
func parseMode(name string) (engine.Mode, error) {
	switch m := engine.Mode(name); m {
	case engine.ModeOff, engine.ModeShadow, engine.ModeWarn, engine.ModeEnforce:
		return m, nil
	}
	return "", fmt.Errorf("mode %q is not one of OFF, SHADOW, WARN, ENFORCE", name)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "dqextprocd: %v\n", err)
	os.Exit(1)
}
