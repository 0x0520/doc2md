package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nullwerke/doc2md/service"
)

// serveCommand implements "doc2md serve": it parses the serve flags and runs the
// service until interrupted.
func serveCommand(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", envOr("DOC2MD_ADDR", "127.0.0.1:8090"), "address to listen on")
	maxMiB := fs.Int64("max-mib", 32, "largest accepted document in MiB")
	apiOnly := fs.Bool("api-only", false, "serve the API only, without the web UI")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `doc2md serve runs the converter as an HTTP service.

usage:
  doc2md serve [flags]

flags:
  -addr <host:port>  address to listen on (default 127.0.0.1:8090, or DOC2MD_ADDR)
  -max-mib <n>       largest accepted document in MiB (default 32)
  -api-only          serve the API only, without the web UI

endpoints:
  GET  /               web UI: add documents, convert, inspect the result
  POST /api/convert    one document (form field "file", optional "verbatim=1") -> Markdown + images
  GET  /api/config     size limit and supported extensions
  GET  /api/health     liveness
`)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	handler := service.Handler(service.Options{
		MaxBytes: *maxMiB << 20,
		NoUI:     *apiOnly,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return listenAndServe(ctx, *addr, handler)
}

// listenAndServe serves handler on addr until ctx is cancelled, then shuts down
// gracefully.
func listenAndServe(ctx context.Context, addr string, handler http.Handler) error {
	// Bind before announcing the address, so a busy port fails at startup instead of
	// leaving a service that looks up but is not.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	// The timeouts keep slow clients from holding connections; the read timeout
	// leaves room for a 32 MiB upload over a slow link.
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	errc := make(chan error, 1)
	go func() {
		fmt.Fprintf(os.Stderr, "doc2md serve: listening on http://%s (Ctrl-C to stop)\n", ln.Addr())
		errc <- srv.Serve(ln)
	}()

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		fmt.Fprintln(os.Stderr, "doc2md serve: stopped")
		return nil
	}
}

// envOr returns the environment variable key, or def if it is unset or empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
