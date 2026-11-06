package secretagent

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

// Client talks to the agent; Start, when set, launches the agent if it is not
// running.
type Client struct {
	Socket string
	Start  func() error
}

func (c *Client) dial() (net.Conn, error) {
	conn, err := net.Dial("unix", c.Socket)
	if err == nil || c.Start == nil {
		return conn, err
	}
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("starting the agent: %w", err)
	}
	for i := 0; i < 50; i++ {
		if conn, err = net.Dial("unix", c.Socket); err == nil {
			return conn, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("the agent does not answer on %s: %w", c.Socket, err)
}

// Do sends a request. A get may wait for the user's approval, so there is no
// deadline.
func (c *Client) Do(req Request) (Response, error) {
	conn, err := c.dial()
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	if !sc.Scan() {
		return Response{}, errors.New("the agent closed the connection")
	}
	var resp Response
	if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
		return Response{}, err
	}
	if resp.Error != "" {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

// Get returns the values of refs (variable → reference).
func (c *Client) Get(reason string, refs map[string]string, account string) (map[string]string, error) {
	resp, err := c.Do(Request{Op: "get", Reason: reason, Refs: refs, Account: account})
	return resp.Values, err
}
