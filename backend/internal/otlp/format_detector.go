package otlp

import (
	"bytes"
	"io"
)

// Format represents the detected data format
type Format int

const (
	FormatUnknown Format = iota
	FormatJSON
	FormatProtobuf
)

// String returns the string representation of the format
func (f Format) String() string {
	switch f {
	case FormatJSON:
		return "JSON"
	case FormatProtobuf:
		return "Protobuf"
	default:
		return "Unknown"
	}
}

// UTF-8 BOM bytes
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// DetectFormat inspects the data to determine if it's JSON or Protobuf.
// It reads the data from the reader and returns both the detected format
// and a new reader containing the original data.
func DetectFormat(r io.Reader) (Format, io.Reader, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return FormatUnknown, nil, err
	}

	if len(data) == 0 {
		return FormatUnknown, bytes.NewReader(data), nil
	}

	format := detectFormatFromBytes(data)
	return format, bytes.NewReader(data), nil
}

// detectFormatFromBytes examines the byte slice to determine the format
func detectFormatFromBytes(data []byte) Format {
	// Strip UTF-8 BOM if present
	if len(data) >= 3 && bytes.HasPrefix(data, utf8BOM) {
		data = data[3:]
	}

	// Skip leading ASCII whitespace (space, tab, newline, carriage return)
	idx := 0
	for idx < len(data) {
		b := data[idx]
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			idx++
		} else {
			break
		}
	}

	if idx >= len(data) {
		return FormatUnknown
	}

	// JSON documents start with '{' or '['. Everything else is treated as
	// protobuf, the OTLP default: OTLP messages usually start with 0x0A
	// (field 1, length-delimited), and a body that is neither fails to decode.
	if firstByte := data[idx]; firstByte == '{' || firstByte == '[' {
		return FormatJSON
	}
	return FormatProtobuf
}
