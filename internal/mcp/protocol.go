package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	mcpProtocolVersion = "2024-11-05"
	maxRPCMessageBytes = 4 * 1024 * 1024 // 4 MiB
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ServeStdio serves newline-delimited JSON-RPC 2.0 MCP requests from r to w,
// logging diagnostics exclusively to errLog.
func (s *Server) ServeStdio(ctx context.Context, r io.Reader, w io.Writer, errLog io.Writer) error {
	if errLog == nil {
		errLog = io.Discard
	}
	reader := bufio.NewReaderSize(r, 64*1024)
	for {
		if ctx.Err() != nil {
			return nil
		}
		line, err := readBoundedLine(reader, maxRPCMessageBytes)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			if errors.Is(err, errLineTooLong) {
				if writeErr := writeRPCResponse(w, rpcResponse{
					JSONRPC: "2.0",
					ID:      json.RawMessage("null"),
					Error:   &rpcError{Code: -32600, Message: "request exceeds 4 MiB limit"},
				}); writeErr != nil {
					return writeErr
				}
				continue
			}
			return err
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		resp, shouldReply := s.handleMessage(trimmed, errLog)
		if !shouldReply {
			continue
		}
		if writeErr := writeRPCResponse(w, resp); writeErr != nil {
			return writeErr
		}
	}
}

var errLineTooLong = errors.New("line exceeds max RPC message size")

func readBoundedLine(r *bufio.Reader, limit int) ([]byte, error) {
	var buf bytes.Buffer
	for {
		chunk, isPrefix, err := r.ReadLine()
		if len(chunk) > 0 {
			if buf.Len()+len(chunk) > limit {
				// Drain remainder of oversized line so framing stays synchronized.
				for isPrefix && err == nil {
					_, isPrefix, err = r.ReadLine()
				}
				return nil, errLineTooLong
			}
			buf.Write(chunk)
		}
		if err != nil {
			if errors.Is(err, io.EOF) && buf.Len() > 0 {
				return buf.Bytes(), nil
			}
			return nil, err
		}
		if !isPrefix {
			return buf.Bytes(), nil
		}
	}
}

func writeRPCResponse(w io.Writer, resp rpcResponse) error {
	encoded, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = w.Write(encoded)
	return err
}

func (s *Server) handleMessage(raw []byte, errLog io.Writer) (resp rpcResponse, shouldReply bool) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: -32700, Message: "parse error"},
		}, true
	}

	isNotification := len(bytes.TrimSpace(req.ID)) == 0
	replyID := req.ID
	if isNotification {
		replyID = json.RawMessage("null")
	}

	if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
		if isNotification {
			return rpcResponse{}, false
		}
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      replyID,
			Error:   &rpcError{Code: -32600, Message: "invalid request"},
		}, true
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(errLog, "grab mcp: recovered panic in %s: %v\n", req.Method, r)
			if isNotification {
				shouldReply = false
				return
			}
			resp = rpcResponse{
				JSONRPC: "2.0",
				ID:      replyID,
				Error:   &rpcError{Code: -32603, Message: "internal error"},
			}
			shouldReply = true
		}
	}()

	switch req.Method {
	case "notifications/initialized", "notifications/cancelled":
		return rpcResponse{}, false

	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if len(bytes.TrimSpace(req.Params)) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return rpcResponse{
					JSONRPC: "2.0",
					ID:      replyID,
					Error:   &rpcError{Code: -32602, Message: "invalid initialize params"},
				}, !isNotification
			}
		}
		ver := strings.TrimSpace(params.ProtocolVersion)
		if ver == "" {
			ver = mcpProtocolVersion
		}
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      replyID,
			Result: map[string]any{
				"protocolVersion": ver,
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "grab-mcp",
					"version": "1.0.0",
				},
			},
		}, !isNotification

	case "ping":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      replyID,
			Result:  map[string]any{},
		}, !isNotification

	case "tools/list":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      replyID,
			Result: map[string]any{
				"tools": toolDefinitions(),
			},
		}, !isNotification

	case "tools/call":
		if isNotification {
			return rpcResponse{}, false
		}
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		dec := json.NewDecoder(bytes.NewReader(req.Params))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&params); err != nil || strings.TrimSpace(params.Name) == "" {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      replyID,
				Error:   &rpcError{Code: -32602, Message: "invalid tools/call params"},
			}, true
		}
		result, rpcErr := s.dispatchTool(params.Name, params.Arguments)
		if rpcErr != nil {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      replyID,
				Error:   rpcErr,
			}, true
		}
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      replyID,
			Result:  result,
		}, true

	default:
		if isNotification {
			return rpcResponse{}, false
		}
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      replyID,
			Error:   &rpcError{Code: -32601, Message: "method not found"},
		}, true
	}
}
