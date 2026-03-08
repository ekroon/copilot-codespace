package delegate

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

type rpcEnvelope struct {
	JSONRPC string           `json:"jsonrpc,omitempty"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	Result json.RawMessage
	Error  *rpcError
}

type rpcClient struct {
	rwc io.ReadWriteCloser

	reader *bufio.Reader

	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int
	pending map[string]chan rpcResponse
	closed  bool
	readErr error
	onEvent func(method string, params json.RawMessage)
}

func newRPCClient(rwc io.ReadWriteCloser) *rpcClient {
	c := &rpcClient{
		rwc:     rwc,
		reader:  bufio.NewReader(rwc),
		pending: make(map[string]chan rpcResponse),
	}
	go c.readLoop()
	return c
}

func (c *rpcClient) SetEventHandler(handler func(method string, params json.RawMessage)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onEvent = handler
}

func (c *rpcClient) Call(ctx context.Context, method string, params any, result any) error {
	id := c.nextRequestID()

	responseCh := make(chan rpcResponse, 1)
	c.mu.Lock()
	c.pending[id] = responseCh
	c.mu.Unlock()

	if err := c.writeMessage(rpcEnvelope{
		JSONRPC: "2.0",
		ID:      rawID(id),
		Method:  method,
		Params:  mustMarshal(params),
	}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		// Context canceled before a response arrived: remove pending entry to
		// prevent deliverResponse from finding it. If deliverResponse already
		// holds a reference to responseCh and is about to send, the drain
		// goroutine below consumes it so the buffered channel isn't stranded.
		// The default case is intentional: if deliverResponse already saw the
		// deleted entry (and thus won't send), the goroutine must not block.
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		go func(ch <-chan rpcResponse) {
			select {
			case <-ch:
			default:
			}
		}(responseCh)
		return ctx.Err()
	case resp := <-responseCh:
		if resp.Error != nil {
			return fmt.Errorf("%s", resp.Error.Message)
		}
		if result == nil || len(resp.Result) == 0 {
			return nil
		}
		return json.Unmarshal(resp.Result, result)
	}
}

func (c *rpcClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	return c.rwc.Close()
}

func (c *rpcClient) nextRequestID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	return strconv.Itoa(c.nextID)
}

func (c *rpcClient) readLoop() {
	for {
		msg, err := c.readMessage()
		if err != nil {
			c.failPending(err)
			return
		}

		switch {
		case msg.Method != "" && msg.ID != nil:
			c.handleRequest(*msg.ID, msg.Method, msg.Params)
		case msg.Method != "":
			c.dispatchEvent(msg.Method, msg.Params)
		case msg.ID != nil:
			c.deliverResponse(*msg.ID, rpcResponse{Result: msg.Result, Error: msg.Error})
		}
	}
}

func (c *rpcClient) handleRequest(id json.RawMessage, method string, params json.RawMessage) {
	switch method {
	case "requestPermission":
		_ = c.reply(id, map[string]any{
			"outcome": map[string]any{
				"outcome": "approved",
			},
		})
	default:
		_ = c.replyError(id, -32601, fmt.Sprintf("unsupported request method %q", method))
	}
}

func (c *rpcClient) dispatchEvent(method string, params json.RawMessage) {
	c.mu.Lock()
	handler := c.onEvent
	c.mu.Unlock()
	if handler != nil {
		handler(method, params)
	}
}

func (c *rpcClient) deliverResponse(id json.RawMessage, response rpcResponse) {
	key := string(id)
	c.mu.Lock()
	ch, ok := c.pending[key]
	if ok {
		delete(c.pending, key)
	}
	c.mu.Unlock()
	if ok {
		ch <- response
	}
}

func (c *rpcClient) failPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readErr = err
	for id, ch := range c.pending {
		ch <- rpcResponse{Error: &rpcError{Message: err.Error()}}
		delete(c.pending, id)
	}
}

func (c *rpcClient) readMessage() (rpcEnvelope, error) {
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return rpcEnvelope{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		var msg rpcEnvelope
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			return rpcEnvelope{}, fmt.Errorf("decoding NDJSON message: %w", err)
		}
		return msg, nil
	}
}

func (c *rpcClient) reply(id json.RawMessage, result any) error {
	return c.writeMessage(rpcEnvelope{
		JSONRPC: "2.0",
		ID:      &id,
		Result:  mustMarshal(result),
	})
}

func (c *rpcClient) replyError(id json.RawMessage, code int, message string) error {
	return c.writeMessage(rpcEnvelope{
		JSONRPC: "2.0",
		ID:      &id,
		Error: &rpcError{
			Code:    code,
			Message: message,
		},
	})
}

func (c *rpcClient) writeMessage(message rpcEnvelope) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	_, err = c.rwc.Write(payload)
	return err
}

func rawID(id string) *json.RawMessage {
	raw := json.RawMessage(id)
	return &raw
}

func mustMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
