package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func pairingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pairing",
		Short: "Manage device pairing (approve, list, revoke)",
	}

	cmd.AddCommand(pairingApproveCmd())
	cmd.AddCommand(pairingListCmd())
	cmd.AddCommand(pairingRevokeCmd())

	return cmd
}

func pairingApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve [code]",
		Short: "Approve a pairing code (interactive if no code given)",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var code string
			if len(args) == 1 {
				code = args[0]
			} else {
				code = pairingInteractiveSelect()
				if code == "" {
					return
				}
			}

			pairingApproveByCode(code)
		},
	}
}

// pairingInteractiveSelect fetches pending pairings from the gateway and lets the user pick one.
func pairingInteractiveSelect() string {
	fmt.Println("Fetching pending pairings...")
	fmt.Println()

	resp, err := gatewayRPC(protocol.MethodPairingList, nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Printf("Failed: %s\n", resp.Error.Message)
		os.Exit(1)
	}

	// Parse the payload into pending list
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		fmt.Printf("Error parsing response: %v\n", err)
		os.Exit(1)
	}

	var listResult struct {
		Pending []struct {
			Code      string `json:"code"`
			SenderID  string `json:"sender_id"`
			Channel   string `json:"channel"`
			ChatID    string `json:"chat_id"`
			AccountID string `json:"account_id"`
			CreatedAt int64  `json:"created_at"`
			ExpiresAt int64  `json:"expires_at"`
		} `json:"pending"`
	}
	if err := json.Unmarshal(raw, &listResult); err != nil {
		fmt.Printf("Error parsing pairing list: %v\n", err)
		os.Exit(1)
	}

	if len(listResult.Pending) == 0 {
		fmt.Println("No pending pairing requests.")
		return ""
	}

	fmt.Printf("Found %d pending request(s).\n\n", len(listResult.Pending))

	// Build select options
	options := make([]SelectOption[string], 0, len(listResult.Pending))
	for _, p := range listResult.Pending {
		createdAt := time.UnixMilli(p.CreatedAt)
		ago := time.Since(createdAt).Truncate(time.Second)
		label := fmt.Sprintf("[%s]  %s / %s  (%s ago)", p.Code, p.Channel, p.SenderID, ago)
		options = append(options, SelectOption[string]{Label: label, Value: p.Code})
	}

	selected, err := promptSelect("Select a pairing request to approve", options, 0)
	if err != nil {
		fmt.Println("Cancelled.")
		return ""
	}

	return selected
}

func pairingApproveByCode(code string) {
	params, _ := json.Marshal(map[string]string{
		"code":       code,
		"approvedBy": "cli-operator",
	})

	resp, err := gatewayRPC(protocol.MethodPairingApprove, params)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if !resp.OK {
		fmt.Printf("Failed: %s\n", resp.Error.Message)
		os.Exit(1)
	}

	fmt.Printf("Pairing approved! Code: %s\n", code)

	if resp.Payload != nil {
		data, _ := json.MarshalIndent(resp.Payload, "", "  ")
		fmt.Println(string(data))
	}
}

func pairingListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List pending and paired devices",
		Run: func(cmd *cobra.Command, args []string) {
			resp, err := gatewayRPC(protocol.MethodPairingList, nil)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}

			if !resp.OK {
				fmt.Printf("Failed: %s\n", resp.Error.Message)
				os.Exit(1)
			}

			data, _ := json.MarshalIndent(resp.Payload, "", "  ")
			fmt.Println(string(data))
		},
	}
}

func pairingRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <channel> <senderId>",
		Short: "Revoke a paired device",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			params, _ := json.Marshal(map[string]string{
				"channel":  args[0],
				"senderId": args[1],
			})

			resp, err := gatewayRPC(protocol.MethodPairingRevoke, params)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}

			if !resp.OK {
				fmt.Printf("Failed: %s\n", resp.Error.Message)
				os.Exit(1)
			}

			fmt.Printf("Revoked pairing for %s/%s\n", args[0], args[1])
		},
	}
}

// gatewayRPC connects to the running gateway, authenticates, sends an RPC call, and returns the response.
func gatewayRPC(method string, params json.RawMessage) (*protocol.ResponseFrame, error) {
	wsURL, err := resolveGatewayWebSocketURL()
	if err != nil {
		return nil, err
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to gateway at %s: %w", wsURL, err)
	}
	defer conn.Close()

	// Step 1: Send connect handshake
	connectParams, _ := json.Marshal(map[string]any{
		"token":    resolveGatewayToken(),
		"protocol": protocol.ProtocolVersion,
	})
	connectReq := protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "cli-connect",
		Method: protocol.MethodConnect,
		Params: connectParams,
	}
	if err := conn.WriteJSON(connectReq); err != nil {
		return nil, fmt.Errorf("send connect: %w", err)
	}

	// Read connect response
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var connectResp protocol.ResponseFrame
	if err := conn.ReadJSON(&connectResp); err != nil {
		return nil, fmt.Errorf("read connect response: %w", err)
	}
	if !connectResp.OK {
		msg := "unknown error"
		if connectResp.Error != nil {
			msg = connectResp.Error.Message
		}
		return nil, fmt.Errorf("connect failed: %s", msg)
	}

	// Step 2: Send the RPC call
	rpcReq := protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "cli-rpc",
		Method: method,
		Params: params,
	}
	if err := conn.WriteJSON(rpcReq); err != nil {
		return nil, fmt.Errorf("send RPC: %w", err)
	}

	// Read response (skip events, find response with matching ID)
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		frameType, _ := protocol.ParseFrameType(msg)
		if frameType == protocol.FrameTypeEvent {
			continue // skip events
		}

		var resp protocol.ResponseFrame
		if err := json.Unmarshal(msg, &resp); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		if resp.ID == "cli-rpc" {
			return &resp, nil
		}
	}
}
