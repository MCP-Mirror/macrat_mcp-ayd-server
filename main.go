package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/macrat/ayd/lib-ayd"
)

var (
	Version = "HEAD"
)

type JsonMap map[string]any

type Request struct {
	JsonRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type ErrorCode int

const (
	ParseError     ErrorCode = -32700
	InvalidRequest ErrorCode = -32600
	MethodNotFound ErrorCode = -32601
	InvalidParams  ErrorCode = -32602
	InternalError  ErrorCode = -32603
)

type ErrorBody struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func NewError(code ErrorCode, message string, data ...any) *ErrorBody {
	return &ErrorBody{
		Code:    code,
		Message: fmt.Sprintf(message, data...),
	}
}

type Response struct {
	JsonRPC string     `json:"jsonrpc"`
	ID      any        `json:"id"`
	Result  any        `json:"result,omitempty"`
	Error   *ErrorBody `json:"error,omitempty"`
}

type RPCConn struct {
	r *json.Decoder
	w *json.Encoder
}

func NewRPCConn(in io.Reader, out io.Writer) RPCConn {
	return RPCConn{
		r: json.NewDecoder(in),
		w: json.NewEncoder(out),
	}
}

func (c RPCConn) Read() (*Request, error) {
	var req Request
	if err := c.r.Decode(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

func (c RPCConn) Write(res Response) error {
	return c.w.Encode(res)
}

type HandlerFunc func(json.RawMessage) (any, *ErrorBody)

type RPCServer struct {
	conn     RPCConn
	handlers map[string]HandlerFunc
}

func NewRPCServer(conn RPCConn) *RPCServer {
	return &RPCServer{
		conn:     conn,
		handlers: make(map[string]HandlerFunc),
	}
}

func (s *RPCServer) SetHandler(method string, handler HandlerFunc) {
	s.handlers[method] = handler
}

func (s *RPCServer) Serve() error {
	for {
		req, err := s.conn.Read()
		if errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}

		handler, ok := s.handlers[req.Method]
		if !ok {
			if req.ID == nil {
				continue
			}
			s.conn.Write(Response{
				JsonRPC: "2.0",
				ID:      req.ID,
				Error: &ErrorBody{
					Code:    MethodNotFound,
					Message: "Method not found",
				},
			})
			continue
		}

		res, errBody := handler(req.Params)
		if errBody != nil {
			s.conn.Write(Response{
				JsonRPC: "2.0",
				ID:      req.ID,
				Error:   errBody,
			})
			continue
		}
		if res != nil {
			s.conn.Write(Response{
				JsonRPC: "2.0",
				ID:      req.ID,
				Result:  res,
			})
		}
	}
}

func IgnoreHandler(params json.RawMessage) (any, *ErrorBody) {
	return nil, nil
}

func PongHandler(params json.RawMessage) (any, *ErrorBody) {
	return JsonMap{}, nil
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    JsonMap    `json:"capabilities"`
	ServerInfo      ServerInfo `json:"serverInfo"`
	Instructions    string     `json:"instructions"`
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Blob string `json:"blob,omitempty"`
}

type ResourceInfo struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

type ResourcesListResult struct {
	Resources []ResourceInfo `json:"resources"`
}

type ResourcesReadRequest struct {
	URI string `json:"uri"`
}

type ResourcesReadResult struct {
	URI     string    `json:"uri"`
	Content []Content `json:"content"`
}

type ToolInfo struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	InputSchema JsonMap `json:"inputSchema"`
}

type ToolsListResult struct {
	Tools []ToolInfo `json:"tools"`
}

type ToolsCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolsCallResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError"`
}

type AydHandlers struct {
	URL *url.URL
}

func (h *AydHandlers) getEndpoint(path string) string {
	u, err := h.URL.Parse(path)
	if err != nil {
		panic(err)
	}
	return u.String()
}

func (h *AydHandlers) InitializeHandler(params json.RawMessage) (any, *ErrorBody) {
	return InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: JsonMap{
			"tools": JsonMap{},
		},
		ServerInfo: ServerInfo{
			Name:    "Ayd Server",
			Version: Version,
		},
		Instructions: fmt.Sprintf("Ayd is a simple service monitoring tool. This server provides status information and monitoring log for the services that are monitoring by Ayd running on %s.", h.URL),
	}, nil
}

func (h *AydHandlers) ToolsList(params json.RawMessage) (any, *ErrorBody) {
	return ToolsListResult{
		Tools: []ToolInfo{
			{
				Name:        "listTargets",
				Description: "List all targets that are monitoring by Ayd.",
				InputSchema: JsonMap{
					"type":       "object",
					"properties": JsonMap{},
				},
			},
			{
				Name:        "getStatusOverview",
				Description: "Get the overview of the status of all targets.",
				InputSchema: JsonMap{
					"type":       "object",
					"properties": JsonMap{},
				},
			},
			{
				Name:        "getTargetStatus",
				Description: "Get the latest status of a specific target URL.",
				InputSchema: JsonMap{
					"type": "object",
					"properties": JsonMap{
						"uri": JsonMap{
							"type":        "string",
							"description": "The target URL that you want to get the status.",
						},
					},
					"required": []string{"uri"},
				},
			},
			{
				Name:        "readLog",
				Description: "Read the monitoring log.",
				InputSchema: JsonMap{
					"type": "object",
					"properties": JsonMap{
						"query": JsonMap{
							"type":        "query",
							"description": "Search query for the log. It can includes target URI, status, and message in the log record.",
						},
						"since": JsonMap{
							"type":        "string",
							"description": "The time that you want to read the log since, in RFC3339 format.",
						},
						"until": JsonMap{
							"type":        "string",
							"description": "The time that you want to read the log until, in RFC3339 format.",
						},
					},
				},
			},
		},
	}, nil
}

func (h *AydHandlers) ToolsCall(params json.RawMessage) (any, *ErrorBody) {
	var req ToolsCallRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, NewError(InvalidParams, "Failed to parse parameters: %v", err)
	}

	switch req.Name {
	case "listTargets":
		return h.ListTargets(req.Arguments)
	case "getStatusOverview":
		return h.GetStatusOverview(req.Arguments)
	case "getTargetStatus":
		return h.GetTargetStatus(req.Arguments)
	case "readLog":
		return h.ReadLog(req.Arguments)
	default:
		return nil, NewError(InvalidParams, "Unknown tool name: %s", req.Name)
	}
}

func (h *AydHandlers) ListTargets(params json.RawMessage) (any, *ErrorBody) {
	resp, err := http.Get(h.getEndpoint("/targets.json"))
	if err != nil {
		return nil, NewError(InternalError, "Failed to fetch targets: %v", err)
	}
	defer resp.Body.Close()

	var targets []string
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return nil, NewError(InternalError, "Failed to load targets: %v", err)
	}

	sort.Strings(targets)

	return ToolsCallResult{
		Content: []Content{
			{
				Type: "text",
				Text: strings.Join(targets, "\n"),
			},
		},
	}, nil
}

func (h *AydHandlers) GetStatusOverview(params json.RawMessage) (any, *ErrorBody) {
	report, err := ayd.Fetch(h.URL)
	if err != nil {
		return nil, NewError(InternalError, "Failed to fetch status from Ayd: %v", err)
	}

	var results []string
	for _, history := range report.ProbeHistory {
		results = append(results, fmt.Sprintf("Target <%s> reported %s in %s at %s", history.Target, history.Status, history.Records[len(history.Records)-1].Latency, history.Updated))
	}

	return ToolsCallResult{
		Content: []Content{
			{
				Type: "text",
				Text: strings.Join(results, "\n"),
			},
		},
	}, nil
}

func (h *AydHandlers) GetTargetStatus(params json.RawMessage) (any, *ErrorBody) {
	var req struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, NewError(InvalidParams, "Failed to parse parameters: %v", err)
	}
	if req.URI == "" {
		return nil, NewError(InvalidParams, "No URIs specified")
	}

	report, err := ayd.Fetch(h.URL)
	if err != nil {
		return nil, NewError(InternalError, "Failed to fetch status from Ayd: %v", err)
	}

	for _, history := range report.ProbeHistory {
		if req.URI == history.Target.String() {
			text, err := json.Marshal(JsonMap{
				"uri":      history.Target.String(),
				"status":   history.Status.String(),
				"last_log": history.Records[len(history.Records)-1],
			})
			if err != nil {
				return ToolsCallResult{
					Content: []Content{
						{
							Type: "text",
							Text: fmt.Sprintf("Failed to marshal response: %v", err),
						},
					},
					IsError: true,
				}, nil
			}
			return ToolsCallResult{
				Content: []Content{
					{
						Type: "text",
						Text: string(text),
					},
				},
			}, nil
		}
	}

	return ToolsCallResult{
		Content: []Content{
			{
				Type: "text",
				Text: fmt.Sprintf("No such target: %s", req.URI),
			},
		},
		IsError: false,
	}, nil
}

func (h *AydHandlers) ReadLog(params json.RawMessage) (any, *ErrorBody) {
	var req struct {
		Query string `json:"query"`
		Since string `json:"since"`
		Until string `json:"until"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, NewError(InvalidParams, "Failed to parse parameters: %v", err)
	}

	u, _ := url.Parse(h.getEndpoint("/log.json"))
	q := u.Query()
	q.Set("query", req.Query)
	q.Set("since", req.Since)
	q.Set("until", req.Until)
	u.RawQuery = q.Encode()

	resp, err := http.Get(u.String())
	if err != nil {
		return nil, NewError(InternalError, "Failed to fetch log: %v", err)
	}

	var logs struct {
		Records []ayd.Record `json:"records"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&logs); err != nil {
		return nil, NewError(InternalError, "Failed to load log: %v", err)
	}

	var results []string
	for _, record := range logs.Records {
		results = append(results, record.String())
	}

	return ToolsCallResult{
		Content: []Content{
			{
				Type: "text",
				Text: strings.Join(results, "\n"),
			},
		},
	}, nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <AYD_URL>\n", os.Args[0])
		os.Exit(1)
	}
	aydURL, err := url.Parse(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid Ayd URL: %v\n", err)
		os.Exit(1)
	}
	handlers := AydHandlers{URL: aydURL}

	server := NewRPCServer(NewRPCConn(os.Stdin, os.Stdout))
	server.SetHandler("initialize", handlers.InitializeHandler)
	server.SetHandler("notifications/initialized", IgnoreHandler)
	server.SetHandler("ping", PongHandler)
	server.SetHandler("tools/list", handlers.ToolsList)
	server.SetHandler("tools/call", handlers.ToolsCall)

	fmt.Fprintf(os.Stderr, "Ayd Server for %s is running on stdio!\n", aydURL)
	err = server.Serve()

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
