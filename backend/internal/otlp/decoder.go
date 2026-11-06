package otlp

import (
	"fmt"
	"io"
	"strings"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"

	"github.com/tobilg/ai-observer/internal/logger"
)

// Decoder interface for OTLP message decoding
type Decoder interface {
	DecodeTraces(r io.Reader) (*coltracepb.ExportTraceServiceRequest, error)
	DecodeMetrics(r io.Reader) (*colmetricspb.ExportMetricsServiceRequest, error)
	DecodeLogs(r io.Reader) (*collogspb.ExportLogsServiceRequest, error)
}

// GetDecoder returns the appropriate decoder based on content type
func GetDecoder(contentType string) (Decoder, error) {
	ct := strings.ToLower(contentType)

	switch {
	case strings.Contains(ct, "application/x-protobuf"):
		return &ProtoDecoder{}, nil
	case strings.Contains(ct, "application/json"):
		return &JSONDecoder{}, nil
	case ct == "":
		// Default to protobuf per OTLP spec
		return &ProtoDecoder{}, nil
	default:
		return nil, fmt.Errorf("unsupported content type: %s", contentType)
	}
}

// GetDecoderWithDetection detects the actual format from the request body
// and returns the appropriate decoder along with a new reader for the body.
// This is useful when the Content-Type header doesn't match the actual content.
// Also returns the detected format for logging purposes.
func GetDecoderWithDetection(r io.Reader, contentType string) (Decoder, io.Reader, error) {
	format, newReader, err := DetectFormat(r)
	if err != nil {
		return nil, nil, fmt.Errorf("detecting format: %w", err)
	}

	// Log if detected format differs from Content-Type header
	expectedFormat := getExpectedFormat(contentType)
	if expectedFormat != FormatUnknown && format != expectedFormat {
		logger.Warn("Format mismatch", "contentType", expectedFormat, "detected", format)
	}

	var decoder Decoder
	switch format {
	case FormatJSON:
		decoder = &JSONDecoder{}
	case FormatProtobuf:
		decoder = &ProtoDecoder{}
	default:
		// Fall back to content type header
		var err error
		decoder, err = GetDecoder(contentType)
		if err != nil {
			return nil, nil, err
		}
	}

	return decoder, newReader, nil
}

// getExpectedFormat returns the expected format based on Content-Type header
func getExpectedFormat(contentType string) Format {
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "application/x-protobuf"):
		return FormatProtobuf
	case strings.Contains(ct, "application/json"):
		return FormatJSON
	default:
		return FormatUnknown
	}
}
