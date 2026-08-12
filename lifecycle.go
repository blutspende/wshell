package wshell

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"time"
)

// RunningServer represents a started wshell HTTP server. It allows an
// application to observe unexpected termination while independently handling
// operating-system signals and application cleanup.
type RunningServer struct {
	echo    echoLifecycle
	address net.Addr
	done    chan struct{}
	err     error
	serve   func() error
	logger  LoggerFunc
}

type echoLifecycle interface {
	Shutdown(context.Context) error
	Close() error
}

// Start prepares the configured service, binds its TCP port synchronously, and
// serves requests in the background. A builder can be started only once.
//
// Binding and configuration failures are returned directly. Observe Done and
// call Wait to handle failures that occur after startup.
func (b *APIBuilder) Start(listenPort int) (*RunningServer, error) {
	b.lifecycleMu.Lock()
	defer b.lifecycleMu.Unlock()

	if b.startAttempted {
		return nil, errors.New("wshell APIBuilder can only be started once")
	}
	b.startAttempted = true

	if err := b.prepare(); err != nil {
		return nil, err
	}

	address := fmt.Sprintf(":%d", listenPort)
	listener, err := (&net.ListenConfig{KeepAlive: 3 * time.Minute}).Listen(
		context.Background(),
		b.echo.ListenerNetwork,
		address,
	)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", address, err)
	}
	b.echo.Listener = listener

	running := &RunningServer{
		echo:    b.echo,
		address: listener.Addr(),
		done:    make(chan struct{}),
		serve: func() error {
			return b.echo.Start(address)
		},
		logger: b.logger,
	}
	go running.run()
	return running, nil
}

// Run is the blocking compatibility wrapper around Start and Wait. New
// applications that need signal handling and cleanup should use Start.
func (b *APIBuilder) Run(listenPort int) error {
	running, err := b.Start(listenPort)
	if err != nil {
		return err
	}
	return running.Wait()
}

func (s *RunningServer) run() {
	defer close(s.done)
	defer func() {
		if recovered := recover(); recovered != nil {
			s.err = fmt.Errorf("HTTP server panic: %v\n%s", recovered, debug.Stack())
			emitLog(s.logger, context.Background(), LogLevelError, "HTTP server panicked", s.err, nil)
		}
	}()

	err := s.serve()
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return
	}
	s.err = fmt.Errorf("HTTP server stopped unexpectedly: %w", err)
	emitLog(s.logger, context.Background(), LogLevelError, "HTTP server stopped unexpectedly", s.err, nil)
}

// Done is closed when the HTTP server has stopped. After it closes, Wait
// returns immediately with the terminal server error, if any.
func (s *RunningServer) Done() <-chan struct{} {
	return s.done
}

// Wait blocks until the HTTP server stops. A graceful shutdown returns nil;
// unexpected Serve errors and recovered Serve panics are returned.
func (s *RunningServer) Wait() error {
	<-s.done
	return s.err
}

// Shutdown gracefully stops the server without interrupting active requests.
// The supplied context bounds the graceful shutdown period.
func (s *RunningServer) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("shutdown context must not be nil")
	}
	return s.echo.Shutdown(ctx)
}

// Close immediately stops the server. Prefer Shutdown during normal process
// termination and reserve Close for forced cleanup.
func (s *RunningServer) Close() error {
	return s.echo.Close()
}

// Address returns the bound listener address. This is useful when Start is
// called with port 0 in tests.
func (s *RunningServer) Address() net.Addr {
	return s.address
}
