package otlp

import (
	"bytes"
	"io"
	"testing"
)

func TestDetectFormat_JSON(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"JSON object", []byte(`{"resourceSpans":[]}`)},
		{"JSON array", []byte(`[{"key":"value"}]`)},
		{"JSON with leading space", []byte(`  {"resourceSpans":[]}`)},
		{"JSON with leading newline", []byte("\n{\"resourceSpans\":[]}")},
		{"JSON with leading tab", []byte("\t{\"resourceSpans\":[]}")},
		{"JSON with UTF-8 BOM", append(utf8BOM, []byte(`{"resourceSpans":[]}`)...)},
		{"JSON with BOM and whitespace", append(utf8BOM, []byte("  {\"resourceSpans\":[]}")...)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format, reader, err := DetectFormat(bytes.NewReader(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if format != FormatJSON {
				t.Errorf("expected FormatJSON, got %v", format)
			}
			// Verify reader contains original data
			data, _ := io.ReadAll(reader)
			if !bytes.Equal(data, tt.input) {
				t.Errorf("reader data mismatch")
			}
		})
	}
}

func TestDetectFormat_Protobuf(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"Protobuf field 1 wire type 2", []byte{0x0A, 0x10, 0x00}},
		{"Protobuf field 2 wire type 2", []byte{0x12, 0x10, 0x00}},
		{"Protobuf with low byte", []byte{0x08, 0x01, 0x10}},
		{"Empty becomes unknown/default", []byte{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format, reader, err := DetectFormat(bytes.NewReader(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Empty data returns Unknown, non-empty protobuf returns Protobuf
			if len(tt.input) > 0 && format != FormatProtobuf {
				t.Errorf("expected FormatProtobuf, got %v", format)
			}
			// Verify reader contains original data
			data, _ := io.ReadAll(reader)
			if !bytes.Equal(data, tt.input) {
				t.Errorf("reader data mismatch")
			}
		})
	}
}

func TestDetectFormat_ContentTypeMismatch(t *testing.T) {
	// JSON body but Content-Type says protobuf - should detect as JSON
	jsonBody := []byte(`{"resourceLogs":[]}`)

	format, reader, err := DetectFormat(bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != FormatJSON {
		t.Errorf("expected FormatJSON for JSON body, got %v", format)
	}

	// Verify we can still read the data
	data, _ := io.ReadAll(reader)
	if !bytes.Equal(data, jsonBody) {
		t.Errorf("reader data mismatch")
	}
}

func TestGetDecoderWithDetection(t *testing.T) {
	tests := []struct {
		name        string
		body        []byte
		contentType string
		wantJSON    bool
	}{
		{
			name:        "JSON body with protobuf content-type",
			body:        []byte(`{"resourceLogs":[]}`),
			contentType: "application/x-protobuf",
			wantJSON:    true,
		},
		{
			name:        "JSON body with JSON content-type",
			body:        []byte(`{"resourceLogs":[]}`),
			contentType: "application/json",
			wantJSON:    true,
		},
		{
			name:        "Protobuf body with protobuf content-type",
			body:        []byte{0x0A, 0x10, 0x00},
			contentType: "application/x-protobuf",
			wantJSON:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoder, reader, err := GetDecoderWithDetection(bytes.NewReader(tt.body), tt.contentType)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			_, isJSON := decoder.(*JSONDecoder)
			if isJSON != tt.wantJSON {
				t.Errorf("expected JSON decoder=%v, got %v", tt.wantJSON, isJSON)
			}

			// Verify reader contains original data
			data, _ := io.ReadAll(reader)
			if !bytes.Equal(data, tt.body) {
				t.Errorf("reader data mismatch")
			}
		})
	}
}

func TestFormatString(t *testing.T) {
	tests := []struct {
		format Format
		want   string
	}{
		{FormatJSON, "JSON"},
		{FormatProtobuf, "Protobuf"},
		{FormatUnknown, "Unknown"},
	}

	for _, tt := range tests {
		if got := tt.format.String(); got != tt.want {
			t.Errorf("Format.String() = %v, want %v", got, tt.want)
		}
	}
}
