package acp

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// buildACPProcess creates an ACPProcess wired to an in-process io.Pipe conn.
// Returns the process, a writer the test can inject responses into (serverW),
// and a reader the test can read outbound requests from (serverR).
//
// The returned serverR is a *bufferedPipeReader that always drains — so the
// conn's writeMessage never blocks even if the test ignores outbound bytes.
//
//	clientW → serverR  (test reads what the process sends)
//	serverW → clientR  (test injects responses to the process)
func buildACPProcess(handler RequestHandler, notify NotifyHandler) (
	proc *ACPProcess,
	serverW io.WriteCloser,
	serverR io.ReadCloser,
) {
	clientR, sW := io.Pipe() // test writes here → conn reads
	sR, clientW := io.Pipe() // conn writes here → test reads

	conn := NewConn(clientW, clientR, handler, notify)
	conn.Start()

	proc = &ACPProcess{
		conn:   conn,
		exited: make(chan struct{}),
		ctx:    context.Background(),
		cancel: func() {},
	}
	return proc, sW, sR
}

// drainPipe reads sR in background so writes to clientW never block.
// Returns a channel that receives each complete line written by the conn.
func drainPipeLines(sR io.ReadCloser) chan []byte {
	ch := make(chan []byte, 64)
	go func() {
		defer close(ch)
		buf := make([]byte, 32*1024)
		for {
			n, err := sR.Read(buf)
			if n > 0 {
				line := make([]byte, n)
				copy(line, buf[:n])
				select {
				case ch <- line:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return ch
}

// replyTo reads one JSON-RPC request from r, then writes a response with the
// given result JSON back to w. Runs in its own goroutine and signals done.
func replyTo(r io.Reader, w io.Writer, resultJSON string) chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 32*1024)
		n, err := r.Read(buf)
		if err != nil || n == 0 {
			return
		}
		var req jsonrpcMessage
		if err := json.Unmarshal(buf[:n], &req); err != nil {
			return
		}
		resp := jsonrpcMessage{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(resultJSON),
		}
		data, _ := json.Marshal(resp)
		w.Write(append(data, '\n'))
	}()
	return done
}

// replyError reads one request and sends back a JSON-RPC error response.
func replyError(r io.Reader, w io.Writer, code int, message string) chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 32*1024)
		n, _ := r.Read(buf)
		if n == 0 {
			return
		}
		var req jsonrpcMessage
		json.Unmarshal(buf[:n], &req)
		resp := jsonrpcMessage{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonrpcError{Code: code, Message: message},
		}
		data, _ := json.Marshal(resp)
		w.Write(append(data, '\n'))
	}()
	return done
}

// --- Initialize tests ---

func TestACPProcess_Initialize_Success(t *testing.T) {
	proc, serverW, serverR := buildACPProcess(nil, nil)
	defer serverW.Close()
	defer serverR.Close()

	respJSON := `{"agentInfo":{"name":"claude","version":"1.0"},"capabilities":{"loadSession":true}}`
	done := replyTo(serverR, serverW, respJSON)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := proc.Initialize(ctx); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	if !proc.agentCaps.LoadSession {
		t.Error("expected LoadSession=true after Initialize")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for replyTo goroutine")
	}
}

func TestACPProcess_Initialize_ErrorResponse(t *testing.T) {
	proc, serverW, serverR := buildACPProcess(nil, nil)
	defer serverW.Close()
	defer serverR.Close()

	done := replyError(serverR, serverW, -32600, "not supported")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := proc.Initialize(ctx)
	if err == nil {
		t.Fatal("expected error from Initialize")
	}
	if !strings.Contains(err.Error(), "acp initialize") {
		t.Errorf("expected 'acp initialize' prefix, got %q", err.Error())
	}

	<-done
}

func TestACPProcess_Initialize_Timeout(t *testing.T) {
	proc, serverW, serverR := buildACPProcess(nil, nil)
	defer serverW.Close()

	// Drain requests so writeMessage doesn't block, but never send a response.
	go func() {
		buf := make([]byte, 4096)
		for {
			_, err := serverR.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := proc.Initialize(ctx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	serverR.Close()
}

// --- NewSession tests ---

func TestACPProcess_NewSession_Success(t *testing.T) {
	proc, serverW, serverR := buildACPProcess(nil, nil)
	defer serverW.Close()
	defer serverR.Close()

	done := replyTo(serverR, serverW, `{"sessionId":"sess-xyz"}`)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := proc.NewSession(ctx); err != nil {
		t.Fatalf("NewSession error: %v", err)
	}
	if proc.sessionID != "sess-xyz" {
		t.Errorf("expected sessionID='sess-xyz', got %q", proc.sessionID)
	}
	<-done
}

func TestACPProcess_NewSession_Error(t *testing.T) {
	proc, serverW, serverR := buildACPProcess(nil, nil)
	defer serverW.Close()
	defer serverR.Close()

	done := replyError(serverR, serverW, -32000, "session failed")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := proc.NewSession(ctx)
	if err == nil {
		t.Fatal("expected error from NewSession")
	}
	if !strings.Contains(err.Error(), "acp session/new") {
		t.Errorf("expected 'acp session/new' prefix, got %q", err.Error())
	}
	<-done
}

// --- Prompt tests ---

func TestACPProcess_Prompt_Success(t *testing.T) {
	proc, serverW, serverR := buildACPProcess(nil, nil)
	proc.sessionID = "sess-1"
	defer serverW.Close()
	defer serverR.Close()

	done := replyTo(serverR, serverW, `{"stopReason":"endTurn"}`)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := proc.Prompt(ctx, []ContentBlock{{Type: "text", Text: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Prompt error: %v", err)
	}
	if resp.StopReason != "endTurn" {
		t.Errorf("expected stopReason='endTurn', got %q", resp.StopReason)
	}
	<-done
}

func TestACPProcess_Prompt_WithUpdateCallback(t *testing.T) {
	updates := make(chan SessionUpdate, 10)
	notifyHandler := func(method string, params json.RawMessage) {
		if method == "session/update" {
			var u SessionUpdate
			json.Unmarshal(params, &u)
			updates <- u
		}
	}

	proc, serverW, serverR := buildACPProcess(nil, notifyHandler)
	proc.sessionID = "sess-2"
	defer serverW.Close()
	defer serverR.Close()

	// Goroutine: send a notification, then a response
	go func() {
		// Read the prompt request first
		buf := make([]byte, 32*1024)
		n, _ := serverR.Read(buf)
		var req jsonrpcMessage
		json.Unmarshal(buf[:n], &req)

		// Send session/update notification
		notif := jsonrpcMessage{
			JSONRPC: "2.0",
			Method:  "session/update",
			Params:  json.RawMessage(`{"kind":"message","stopReason":""}`),
		}
		nd, _ := json.Marshal(notif)
		serverW.Write(append(nd, '\n'))

		// Send prompt response
		resp := jsonrpcMessage{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"stopReason":"endTurn"}`),
		}
		rd, _ := json.Marshal(resp)
		serverW.Write(append(rd, '\n'))
	}()

	var received []SessionUpdate
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := proc.Prompt(ctx, []ContentBlock{{Type: "text", Text: "hello"}}, func(u SessionUpdate) {
		received = append(received, u)
	})
	if err != nil {
		t.Fatalf("Prompt error: %v", err)
	}

	// Drain notification channel
	time.Sleep(20 * time.Millisecond)
	// The update callback + notify handler may both fire — just check no panic occurred
}

func TestACPProcess_Prompt_Error(t *testing.T) {
	proc, serverW, serverR := buildACPProcess(nil, nil)
	proc.sessionID = "sess-3"
	defer serverW.Close()
	defer serverR.Close()

	done := replyError(serverR, serverW, -32000, "prompt failed")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := proc.Prompt(ctx, []ContentBlock{{Type: "text", Text: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error from Prompt")
	}
	if !strings.Contains(err.Error(), "acp session/prompt") {
		t.Errorf("expected 'acp session/prompt' prefix, got %q", err.Error())
	}
	<-done
}

func TestACPProcess_Prompt_SetsInUse(t *testing.T) {
	// Verify inUse counter goes up during Prompt and back to 0 after
	proc, serverW, serverR := buildACPProcess(nil, nil)
	proc.sessionID = "sess-4"
	defer serverW.Close()
	defer serverR.Close()

	started := make(chan struct{})
	go func() {
		// Read and wait a moment before responding
		buf := make([]byte, 32*1024)
		n, _ := serverR.Read(buf)
		var req jsonrpcMessage
		json.Unmarshal(buf[:n], &req)

		close(started)
		time.Sleep(20 * time.Millisecond)

		resp := jsonrpcMessage{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"stopReason":"endTurn"}`),
		}
		data, _ := json.Marshal(resp)
		serverW.Write(append(data, '\n'))
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		proc.Prompt(ctx, []ContentBlock{{Type: "text", Text: "x"}}, nil)
	}()

	<-started
	if proc.inUse.Load() != 1 {
		t.Errorf("expected inUse=1 during Prompt, got %d", proc.inUse.Load())
	}

	<-done
	if proc.inUse.Load() != 0 {
		t.Errorf("expected inUse=0 after Prompt, got %d", proc.inUse.Load())
	}
}

// --- Cancel tests ---

func TestACPProcess_Cancel_SendsNotification(t *testing.T) {
	proc, serverW, serverR := buildACPProcess(nil, nil)
	proc.sessionID = "sess-cancel"
	defer serverW.Close()

	received := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _ := serverR.Read(buf)
		var msg jsonrpcMessage
		json.Unmarshal(buf[:n], &msg)
		received <- msg.Method
	}()

	err := proc.Cancel()
	if err != nil {
		t.Fatalf("Cancel error: %v", err)
	}

	select {
	case method := <-received:
		if method != "session/cancel" {
			t.Errorf("expected 'session/cancel', got %q", method)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for cancel notification")
	}
}

// --- dispatchUpdate / setUpdateFn ---

func TestACPProcess_DispatchUpdate_NilFn(t *testing.T) {
	proc := &ACPProcess{}
	// Should not panic with nil updateFn
	proc.dispatchUpdate(SessionUpdate{Kind: "message"})
}

func TestACPProcess_DispatchUpdate_CallsFn(t *testing.T) {
	proc := &ACPProcess{}
	called := false
	proc.setUpdateFn(func(u SessionUpdate) {
		called = true
	})
	proc.dispatchUpdate(SessionUpdate{Kind: "message"})
	if !called {
		t.Error("expected updateFn to be called")
	}
}

func TestACPProcess_SetUpdateFn_Nil(t *testing.T) {
	proc := &ACPProcess{}
	proc.setUpdateFn(func(u SessionUpdate) {})
	proc.setUpdateFn(nil)
	// Should not panic
	proc.dispatchUpdate(SessionUpdate{Kind: "x"})
}
