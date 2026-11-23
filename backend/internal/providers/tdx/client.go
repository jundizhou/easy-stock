// Package tdx reads Shanghai and Shenzhen quotes using the TongdaXin MAC protocol.
package tdx

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/bensema/gotdx"
	"github.com/bensema/gotdx/proto"
)

type Client struct {
	hosts      []string
	slots      chan struct{}
	mu         sync.Mutex
	next       int
	retryAfter []time.Time
}

func NewClient() *Client {
	hosts := gotdx.MACHostAddresses()
	return &Client{hosts: hosts, slots: make(chan struct{}, 8), retryAfter: make([]time.Time, len(hosts))}
}

// The SDK's message constructors share a sequence counter. Serialize construction
// only. Each exchange owns its socket and observes the caller's cancellation.
var messageMu sync.Mutex

func (c *Client) host() (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for offset := range c.hosts {
		i := (c.next + offset) % len(c.hosts)
		if !time.Now().Before(c.retryAfter[i]) {
			return i, true
		}
	}
	return 0, false
}

type message interface {
	proto.RequestBuilder
	proto.ResponseParser
}

func (c *Client) request(parent context.Context, method uint16, msg message) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	i, ok := c.host()
	if !ok {
		return errors.New("通达信行情连接暂不可用")
	}
	body, err := msg.BuildRequest()
	if err != nil {
		return err
	}
	header, payload, err := exchange(ctx, c.hosts[i], body, method)
	if parent.Err() != nil {
		return parent.Err()
	}
	if err != nil {
		c.mu.Lock()
		c.next = (i + 1) % len(c.hosts)
		c.retryAfter[i] = time.Now().Add(30 * time.Second)
		c.mu.Unlock()
		return fmt.Errorf("通达信行情请求失败: %w", err)
	}
	return msg.ParseResponse(&header, payload)
}

func exchange(ctx context.Context, host string, request []byte, method uint16) (proto.RespHeader, []byte, error) {
	var header proto.RespHeader
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", host)
	if err != nil {
		return header, nil, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return header, nil, err
		}
	}
	if _, err := conn.Write(request); err != nil {
		return header, nil, err
	}
	if err := binary.Read(conn, binary.LittleEndian, &header); err != nil {
		return header, nil, err
	}
	if header.Method != method || header.ZipSize == 0 || header.UnZipSize == 0 {
		return header, nil, errors.New("通达信返回无效行情帧")
	}
	payload := make([]byte, int(header.ZipSize))
	if _, err := io.ReadFull(conn, payload); err != nil {
		return header, nil, err
	}
	if header.ZipSize != header.UnZipSize {
		r, err := zlib.NewReader(bytes.NewReader(payload))
		if err != nil {
			return header, nil, err
		}
		defer r.Close()
		payload, err = io.ReadAll(io.LimitReader(r, int64(header.UnZipSize)+1))
		if err != nil {
			return header, nil, err
		}
		if len(payload) != int(header.UnZipSize) {
			return header, nil, errors.New("通达信行情长度不符")
		}
	}
	return header, payload, nil
}
