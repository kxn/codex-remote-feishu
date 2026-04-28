package wrapper

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
)

type blockingReader struct {
	done chan struct{}
	err  error
}

func newBlockingReader(err error) *blockingReader {
	return &blockingReader{
		done: make(chan struct{}),
		err:  err,
	}
}

func (r *blockingReader) Read(_ []byte) (int, error) {
	<-r.done
	return 0, r.err
}

func (r *blockingReader) Close() {
	select {
	case <-r.done:
	default:
		close(r.done)
	}
}

func TestRestoreChildSessionContextClearsPendingStateOnCancelBeforeQueue(t *testing.T) {
	app := New(Config{
		InstanceID: "inst-1",
	})
	runtime, ok := app.runtime.(*codexBackendRuntime)
	if !ok {
		t.Fatalf("expected codex runtime, got %T", app.runtime)
	}
	if _, err := runtime.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("seed thread/resume: %v", err)
	}

	writeCh := make(chan []byte)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- app.restoreChildSessionContext(ctx, "cmd-restart-1", writeCh, &relayws.Client{}, nil)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected restore cancellation error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for restore cancellation")
	}
}

func TestStdoutLoopIgnoresClosedPipeAfterSessionCancel(t *testing.T) {
	reader := newBlockingReader(errors.New("read |0: file already closed"))
	runtime, ok := newBackendRuntime(Config{InstanceID: "inst-1"}).(*codexBackendRuntime)
	if !ok {
		t.Fatal("expected codex runtime")
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	done := make(chan struct{})

	go func() {
		stdoutLoop(ctx, reader, io.Discard, make(chan []byte, 1), runtime, nil, newCommandResponseTracker(), newRuntimeTurnTracker(), nil, 0, errCh, nil, nil, nil)
		close(done)
	}()

	cancel()
	reader.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stdoutLoop to exit")
	}

	select {
	case err := <-errCh:
		t.Fatalf("expected canceled session to suppress closed-pipe error, got %v", err)
	default:
	}
}
