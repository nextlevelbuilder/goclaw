package mcp

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
)

type sseEvent struct {
	Event string
	Data  []byte
}

func readSSE(ctx context.Context, r io.Reader, onEvent func(sseEvent) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	var eventName string
	var dataLines [][]byte

	flush := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			return nil
		}
		raw := bytes.TrimSpace(bytes.Join(dataLines, []byte("\n")))
		dataLines = nil
		event := sseEvent{
			Event: eventName,
			Data:  append([]byte(nil), raw...),
		}
		eventName = ""
		if len(event.Data) == 0 {
			return nil
		}
		return onEvent(event)
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value, ok := strings.Cut(line, ":")
		if ok {
			value = strings.TrimPrefix(value, " ")
		} else {
			value = ""
		}

		switch field {
		case "event":
			eventName = value
		case "data":
			dataLines = append(dataLines, []byte(value))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(dataLines) > 0 {
		return flush()
	}
	return nil
}
