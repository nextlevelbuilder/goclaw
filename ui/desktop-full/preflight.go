//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// postgresAddr extracts host and port from a URL or key=value Postgres DSN.
func postgresAddr(dsn string) (host, port string, err error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return "", "", errors.New("empty DSN")
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, perr := url.Parse(dsn)
		if perr != nil {
			return "", "", fmt.Errorf("parse DSN: %w", perr)
		}
		host, port = u.Hostname(), u.Port()
	} else {
		for _, field := range strings.Fields(dsn) {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch k {
			case "host":
				host = strings.Trim(v, "'")
			case "port":
				port = strings.Trim(v, "'")
			}
		}
	}
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "5432"
	}
	return host, port, nil
}

// waitForTCP polls until addr accepts a connection, ctx ends or timeout passes.
func waitForTCP(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for {
		d := net.Dialer{Timeout: 2 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			conn.Close()
			return nil
		}
		last = err
		if time.Now().After(deadline) {
			return fmt.Errorf("%s unreachable: %w", addr, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
