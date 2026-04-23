package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func runClientMode(cfg *config.Config, addr, agentName, message, sessionKey string) {
	wsURL := fmt.Sprintf("ws://%s/ws", addr)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WebSocket connect failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Authenticate
	if err := wsConnect(conn, cfg.Gateway.Token); err != nil {
		fmt.Fprintf(os.Stderr, "Gateway auth failed: %v\n", err)
		os.Exit(1)
	}

	agentCfg := cfg.ResolveAgent(agentName)

	if message != "" {
		// One-shot mode
		resp, err := wsChatSend(conn, agentName, sessionKey, message)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(resp)
		return
	}

	// Interactive REPL
	fmt.Fprintf(os.Stderr, "\nGoClaw Interactive Chat (agent: %s, model: %s)\n", agentName, agentCfg.Model)
	fmt.Fprintf(os.Stderr, "Session: %s\n", sessionKey)
	fmt.Fprintf(os.Stderr, "Type \"exit\" to quit, \"/new\" for new session\n\n")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, "You: ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			fmt.Fprintln(os.Stderr, "Goodbye!")
			return
		}
		if input == "/new" {
			sessionKey = sessions.BuildSessionKey(agentName, "cli", sessions.PeerDirect, uuid.NewString()[:8])
			fmt.Fprintf(os.Stderr, "New session: %s\n\n", sessionKey)
			continue
		}

		resp, err := wsChatSend(conn, agentName, sessionKey, input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
			continue
		}
		fmt.Printf("\n%s\n\n", resp)
	}
}

// wsConnect sends the connect RPC and waits for auth response.
func wsConnect(conn *websocket.Conn, token string) error {
	params := map[string]string{}
	userId := os.Getenv("GOCLAW_USER_ID")
	if userId == "" { userId = "system" }
	params["user_id"] = userId
	if token != "" {
		params["token"] = token
	}
	paramsJSON, _ := json.Marshal(params)

	reqFrame := protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "connect-1",
		Method: protocol.MethodConnect,
		Params: paramsJSON,
	}

	if err := conn.WriteJSON(reqFrame); err != nil {
		return fmt.Errorf("send connect: %w", err)
	}

	var resp protocol.ResponseFrame
	if err := conn.ReadJSON(&resp); err != nil {
		return fmt.Errorf("read connect response: %w", err)
	}
	if !resp.OK {
		if resp.Error != nil {
			return fmt.Errorf("connect rejected: %s", resp.Error.Message)
		}
		return fmt.Errorf("connect rejected")
	}

	return nil
}

// wsChatSend sends a chat.send RPC and waits for the response,
// displaying events (tool calls, chunks) in real-time.
func wsChatSend(conn *websocket.Conn, agentID, sessionKey, message string) (string, error) {
	reqID := uuid.NewString()[:8]
	params, _ := json.Marshal(map[string]any{
		"message":    message,
		"agentId":    agentID,
		"sessionKey": sessionKey,
		"stream":     true,
	})

	reqFrame := protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     reqID,
		Method: protocol.MethodChatSend,
		Params: params,
	}

	if err := conn.WriteJSON(reqFrame); err != nil {
		return "", fmt.Errorf("send chat: %w", err)
	}

	// Read frames until we get our response
	var finalContent string
	for {
		_, rawMsg, err := conn.ReadMessage()
		if err != nil {
			return "", fmt.Errorf("read: %w", err)
		}

		frameType, _ := protocol.ParseFrameType(rawMsg)

		switch frameType {
		case protocol.FrameTypeResponse:
			var resp protocol.ResponseFrame
			if err := json.Unmarshal(rawMsg, &resp); err != nil {
				continue
			}
			if resp.ID != reqID {
				continue // response for a different request
			}
			if !resp.OK {
				if resp.Error != nil {
					return "", fmt.Errorf("agent error: %s", resp.Error.Message)
				}
				return "", fmt.Errorf("agent error (unknown)")
			}
			// Extract content from payload
			if payload, ok := resp.Payload.(map[string]any); ok {
				if content, ok := payload["content"].(string); ok && content != "" {
					finalContent = content
				}
			}
			return finalContent, nil

		case protocol.FrameTypeEvent:
			var evt protocol.EventFrame
			if err := json.Unmarshal(rawMsg, &evt); err != nil {
				continue
			}
			handleCLIEvent(evt)
		}
	}
}

// handleCLIEvent displays agent events in the terminal.
func handleCLIEvent(evt protocol.EventFrame) {
	payload, ok := evt.Payload.(map[string]any)
	if !ok {
		return
	}

	evtType, _ := payload["type"].(string)

	switch evt.Event {
	case protocol.EventAgent:
		switch evtType {
		case protocol.AgentEventToolCall:
			if p, ok := payload["payload"].(map[string]any); ok {
				name, _ := p["toolName"].(string)
				if name == "" {
					name, _ = p["name"].(string)
				}
				fmt.Fprintf(os.Stderr, "  [tool] %s\n", name)
			}
		case protocol.AgentEventToolResult:
			if p, ok := payload["payload"].(map[string]any); ok {
				isErr, _ := p["is_error"].(bool)
				name, _ := p["toolName"].(string)
				if name == "" {
					name, _ = p["name"].(string)
				}
				if isErr {
					fmt.Fprintf(os.Stderr, "  [tool] %s -> error\n", name)
				}
			}
		}

	case protocol.EventChat:
		switch evtType {
		case protocol.ChatEventChunk:
			if content, ok := payload["content"].(string); ok {
				fmt.Print(content)
			}
		}
	}
}
