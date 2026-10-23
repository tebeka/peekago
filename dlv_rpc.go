package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"sync"
	"time"
)

type rpcClient struct {
	conn   net.Conn
	reader *json.Decoder
	writer *json.Encoder
	mu     sync.Mutex
	nextID int64
}

func dialRPC(ctx context.Context, addr string) (*rpcClient, error) {
	deadline := time.Now().Add(time.Duration(debugServerWait) * time.Second)
	var lastErr error

	for time.Now().Before(deadline) {
		var dialer net.Dialer
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			return &rpcClient{
				conn:   conn,
				reader: json.NewDecoder(conn),
				writer: json.NewEncoder(conn),
				nextID: 1,
			}, nil
		}

		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}

	if lastErr == nil {
		lastErr = errors.New("timed out dialing delve")
	}

	return nil, lastErr
}

func (c *rpcClient) close() {
	if c == nil || c.conn == nil {
		return
	}

	_ = c.call("RPCServer.Detach", struct {
		Kill bool
	}{Kill: true}, &struct{}{})
	_ = c.conn.Close()
}

func (c *rpcClient) setAPIVersion(version int) error {
	return c.call("RPCServer.SetApiVersion", struct {
		APIVersion int
	}{APIVersion: version}, &struct{}{})
}

func (c *rpcClient) createBreakpoint(loc string) error {
	var out struct {
		Breakpoint breakpoint `json:"Breakpoint"`
	}

	return c.call("RPCServer.CreateBreakpoint", struct {
		LocExpr string
	}{LocExpr: loc}, &out)
}

func (c *rpcClient) command(ctx context.Context, cmd debuggerCommand) (*debuggerState, error) {
	var out struct {
		State debuggerState `json:"State"`
	}

	if err := c.callContext(ctx, "RPCServer.Command", cmd, &out); err != nil {
		return nil, err
	}

	return &out.State, nil
}

func (c *rpcClient) stacktrace(ctx context.Context, goroutineID int64) ([]stackFrame, error) {
	var out struct {
		Locations []stackFrame
	}

	err := c.callContext(ctx, "RPCServer.Stacktrace", struct {
		ID    int64 `json:"Id"`
		Depth int
		Full  bool
		Cfg   *loadConfig
	}{
		ID:    goroutineID,
		Depth: stacktraceDepth,
		Full:  true,
		Cfg:   defaultLoadConfig(),
	}, &out)
	if err != nil {
		return nil, err
	}

	return out.Locations, nil
}

func (c *rpcClient) packageVars(ctx context.Context) ([]variable, error) {
	var out struct {
		Variables []variable
	}

	err := c.callContext(ctx, "RPCServer.ListPackageVars", struct {
		Filter string
		Cfg    loadConfig
	}{
		Filter: "",
		Cfg:    *defaultLoadConfig(),
	}, &out)
	if err != nil {
		return nil, err
	}

	return out.Variables, nil
}

func (c *rpcClient) call(method string, params any, reply any) error {
	return c.callContext(context.Background(), method, params, reply)
}

func (c *rpcClient) callContext(ctx context.Context, method string, params any, reply any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetDeadline(deadline)
	} else {
		_ = c.conn.SetDeadline(time.Now().Add(time.Duration(commandTimeout) * time.Second))
	}
	defer c.conn.SetDeadline(time.Time{})

	req := rpcRequest{
		Method: method,
		Params: []any{params},
		ID:     c.nextID,
	}
	c.nextID++

	if err := c.writer.Encode(req); err != nil {
		return err
	}

	var resp rpcResponse
	if err := c.reader.Decode(&resp); err != nil {
		return err
	}

	if resp.Error != nil {
		return errors.New(resp.Error.Message)
	}

	if reply == nil || len(resp.Result) == 0 || bytes.Equal(resp.Result, []byte("null")) {
		return nil
	}

	return json.Unmarshal(resp.Result, reply)
}

type rpcRequest struct {
	Method string `json:"method"`
	Params []any  `json:"params"`
	ID     int64  `json:"id"`
}

type rpcResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type debuggerCommand struct {
	Name        string `json:"name"`
	GoroutineID int64  `json:"goroutineID,omitempty"`
}

type debuggerState struct {
	CurrentThread     *thread    `json:"currentThread,omitempty"`
	SelectedGoroutine *goroutine `json:"currentGoroutine,omitempty"`
	Exited            bool       `json:"exited"`
	ExitStatus        int        `json:"exitStatus"`
}

type thread struct {
	File        string    `json:"file"`
	Line        int       `json:"line"`
	Function    *function `json:"function,omitempty"`
	GoroutineID int64     `json:"goroutineID"`
}

type goroutine struct {
	ID             int64    `json:"id"`
	UserCurrentLoc location `json:"userCurrentLoc"`
}

type breakpoint struct {
	ID int `json:"id"`
}

type stackFrame struct {
	File      string     `json:"File"`
	Line      int        `json:"Line"`
	Function  *function  `json:"Function"`
	Locals    []variable `json:"Locals"`
	Arguments []variable `json:"Arguments"`
}

type location struct {
	File     string    `json:"file"`
	Line     int       `json:"line"`
	Function *function `json:"function,omitempty"`
}

type function struct {
	Name_ string `json:"name"`
}

type variable struct {
	Name       string       `json:"name"`
	Addr       uint64       `json:"addr"`
	OnlyAddr   bool         `json:"onlyAddr"`
	Type       string       `json:"type"`
	Kind       reflect.Kind `json:"kind"`
	Value      string       `json:"value"`
	Len        int64        `json:"len"`
	Cap        int64        `json:"cap"`
	Base       uint64       `json:"base"`
	Unreadable string       `json:"unreadable"`
	Children   []variable   `json:"children"`
}

type loadConfig struct {
	FollowPointers     bool `json:"FollowPointers"`
	MaxVariableRecurse int  `json:"MaxVariableRecurse"`
	MaxStringLen       int  `json:"MaxStringLen"`
	MaxArrayValues     int  `json:"MaxArrayValues"`
	MaxStructFields    int  `json:"MaxStructFields"`
}
