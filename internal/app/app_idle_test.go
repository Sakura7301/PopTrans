package app

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestMainWindowOpenPreventsBackendIdleStop(t *testing.T) {
	var stopped atomic.Int32
	app := &App{
		backendIdleTimeout: 40 * time.Millisecond,
		backendStopHook: func() {
			stopped.Add(1)
		},
	}
	t.Cleanup(app.cancelBackendIdleStop)

	app.setMainWindowOpen(true)
	app.beginBackendUse()
	app.endBackendUse()

	time.Sleep(110 * time.Millisecond)
	if stopped.Load() != 0 {
		t.Fatalf("expected backend not to idle-stop while main window is open, stopped=%d", stopped.Load())
	}

	app.setMainWindowOpen(false)
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if stopped.Load() == 1 {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("expected backend to idle-stop after main window closed, stopped=%d", stopped.Load())
}

func TestBeginBackendUseWaitsForInFlightIdleStop(t *testing.T) {
	stopEntered := make(chan struct{})
	allowStopExit := make(chan struct{})
	app := &App{
		backendIdleTimeout: 15 * time.Millisecond,
		backendStopHook: func() {
			close(stopEntered)
			<-allowStopExit
		},
	}
	t.Cleanup(app.cancelBackendIdleStop)

	app.beginBackendUse()
	app.endBackendUse()

	select {
	case <-stopEntered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for idle stop to begin")
	}

	beginReturned := make(chan struct{})
	go func() {
		app.beginBackendUse()
		close(beginReturned)
	}()

	select {
	case <-beginReturned:
		t.Fatal("beginBackendUse returned while idle stop was still in flight")
	case <-time.After(60 * time.Millisecond):
	}

	close(allowStopExit)
	select {
	case <-beginReturned:
	case <-time.After(time.Second):
		t.Fatal("beginBackendUse did not unblock after idle stop finished")
	}
	app.endBackendUse()
}
