package feishu

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// --- Minimal protobuf wire format ---
// Implements just enough protobuf encoding/decoding for the Frame message.
// No external protobuf library needed.

type wsHeader struct {
	Key   string
	Value string
}

type wsFrame struct {
	SeqID           uint64
	LogID           uint64
	Service         int32
	Method          int32
	Headers         []wsHeader
	PayloadEncoding string
	PayloadType     string
	Payload         []byte
	LogIDNew        string
}

func (f *wsFrame) headerMap() map[string]string {
	m := make(map[string]string, len(f.Headers))
	for _, h := range f.Headers {
		m[h.Key] = h.Value
	}
	return m
}

// marshalFrame encodes a wsFrame to protobuf wire format.
func marshalFrame(f *wsFrame) []byte {
	var buf bytes.Buffer

	if f.SeqID != 0 {
		pbWriteVarintField(&buf, 1, f.SeqID)
	}
	if f.LogID != 0 {
		pbWriteVarintField(&buf, 2, f.LogID)
	}
	if f.Service != 0 {
		pbWriteVarintField(&buf, 3, uint64(f.Service))
	}
	if f.Method != 0 {
		pbWriteVarintField(&buf, 4, uint64(f.Method))
	}
	for _, h := range f.Headers {
		// Embedded message: Header { key=1, value=2 }
		var hbuf bytes.Buffer
		pbWriteBytesField(&hbuf, 1, []byte(h.Key))
		pbWriteBytesField(&hbuf, 2, []byte(h.Value))
		pbWriteBytesField(&buf, 5, hbuf.Bytes())
	}
	if f.PayloadEncoding != "" {
		pbWriteBytesField(&buf, 6, []byte(f.PayloadEncoding))
	}
	if f.PayloadType != "" {
		pbWriteBytesField(&buf, 7, []byte(f.PayloadType))
	}
	if len(f.Payload) > 0 {
		pbWriteBytesField(&buf, 8, f.Payload)
	}
	if f.LogIDNew != "" {
		pbWriteBytesField(&buf, 9, []byte(f.LogIDNew))
	}

	return buf.Bytes()
}

// unmarshalFrame decodes a protobuf wire-format message into wsFrame.
func unmarshalFrame(data []byte) (*wsFrame, error) {
	f := &wsFrame{}
	r := bytes.NewReader(data)

	for r.Len() > 0 {
		tag, err := binary.ReadUvarint(r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("read tag: %w", err)
		}

		fieldNum := tag >> 3
		wireType := tag & 0x7

		switch wireType {
		case 0: // varint
			val, err := binary.ReadUvarint(r)
			if err != nil {
				return nil, fmt.Errorf("read varint field %d: %w", fieldNum, err)
			}
			switch fieldNum {
			case 1:
				f.SeqID = val
			case 2:
				f.LogID = val
			case 3:
				f.Service = int32(val)
			case 4:
				f.Method = int32(val)
			}

		case 2: // length-delimited
			length, err := binary.ReadUvarint(r)
			if err != nil {
				return nil, fmt.Errorf("read length field %d: %w", fieldNum, err)
			}
			buf := make([]byte, length)
			if _, err := io.ReadFull(r, buf); err != nil {
				return nil, fmt.Errorf("read bytes field %d: %w", fieldNum, err)
			}
			switch fieldNum {
			case 5: // Header (embedded message)
				h, err := unmarshalHeader(buf)
				if err == nil {
					f.Headers = append(f.Headers, h)
				}
			case 6:
				f.PayloadEncoding = string(buf)
			case 7:
				f.PayloadType = string(buf)
			case 8:
				f.Payload = buf
			case 9:
				f.LogIDNew = string(buf)
			}

		default:
			return nil, fmt.Errorf("unsupported wire type %d for field %d", wireType, fieldNum)
		}
	}

	return f, nil
}

func unmarshalHeader(data []byte) (wsHeader, error) {
	var h wsHeader
	r := bytes.NewReader(data)

	for r.Len() > 0 {
		tag, err := binary.ReadUvarint(r)
		if err != nil {
			break
		}
		fieldNum := tag >> 3
		wireType := tag & 0x7

		if wireType != 2 {
			return h, fmt.Errorf("header: unexpected wire type %d", wireType)
		}

		length, err := binary.ReadUvarint(r)
		if err != nil {
			return h, err
		}
		buf := make([]byte, length)
		if _, err := io.ReadFull(r, buf); err != nil {
			return h, err
		}

		switch fieldNum {
		case 1:
			h.Key = string(buf)
		case 2:
			h.Value = string(buf)
		}
	}

	return h, nil
}

// --- Protobuf encoding helpers ---

func pbWriteVarintField(w *bytes.Buffer, fieldNum int, val uint64) {
	tag := uint64(fieldNum << 3) // wire type 0 = varint
	pbWriteUvarint(w, tag)
	pbWriteUvarint(w, val)
}

func pbWriteBytesField(w *bytes.Buffer, fieldNum int, data []byte) {
	tag := uint64(fieldNum<<3 | 2) // wire type 2 = length-delimited
	pbWriteUvarint(w, tag)
	pbWriteUvarint(w, uint64(len(data)))
	w.Write(data)
}

func pbWriteUvarint(w *bytes.Buffer, val uint64) {
	var buf [10]byte
	n := binary.PutUvarint(buf[:], val)
	w.Write(buf[:n])
}
