// Command remotehands serves the ConnectRPC API for remote execution.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/mvp-joe/remote-hands/gen/remotehands/v1/remotehandsv1connect"
	"github.com/mvp-joe/remote-hands/worker"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", "", "TCP listen address (default \"0.0.0.0:19051\")")
	socket := flag.String("socket", "", "Unix socket path (alternative to --listen)")
	home := flag.String("home", "", "Working directory root (required)")
	authTokenEnv := flag.String("auth-token-env", "", "Env var name containing bearer token (optional)")
	flag.Parse()

	// Validate flags.
	if *home == "" {
		return fmt.Errorf("--home is required")
	}
	if *listen != "" && *socket != "" {
		return fmt.Errorf("--listen and --socket are mutually exclusive")
	}

	// Default to TCP if neither is specified.
	network := "tcp"
	addr := "0.0.0.0:19051"
	if *listen != "" {
		addr = *listen
	}
	if *socket != "" {
		network = "unix"
		addr = *socket
	}

	// Resolve auth token from env var.
	var authToken string
	if *authTokenEnv != "" {
		authToken = os.Getenv(*authTokenEnv)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	gitAuthorName := os.Getenv("GIT_AUTHOR_NAME")
	gitAuthorEmail := os.Getenv("GIT_AUTHOR_EMAIL")

	var svc *worker.Service
	if gitAuthorName != "" || gitAuthorEmail != "" {
		var err error
		svc, err = worker.NewServiceWithGitAuth(*home, logger, worker.ServiceGitOptions{
			AuthorName:  gitAuthorName,
			AuthorEmail: gitAuthorEmail,
		})
		if err != nil {
			return fmt.Errorf("create service: %w", err)
		}
	} else {
		var err error
		svc, err = worker.NewService(*home, logger)
		if err != nil {
			return fmt.Errorf("create service: %w", err)
		}
	}

	// Build interceptors.
	var opts []connect.HandlerOption
	if authToken != "" {
		opts = append(opts, connect.WithInterceptors(
			worker.NewAuthInterceptor(authToken),
			worker.NewStreamAuthInterceptor(authToken),
		))
	}

	mux := http.NewServeMux()
	path, handler := remotehandsv1connect.NewServiceHandler(svc, opts...)
	mux.Handle(path, handler)

	server := &http.Server{
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}

	// Listen.
	ln, err := net.Listen(network, addr)
	if err != nil {
		return fmt.Errorf("listen %s %s: %w", network, addr, err)
	}
	logger.Info("serving", "network", network, "addr", ln.Addr().String())

	// Graceful shutdown on SIGTERM/SIGINT.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
		svc.Close(context.Background())
		return server.Close()
	case err := <-errCh:
		svc.Close(context.Background())
		return err
	}
}
