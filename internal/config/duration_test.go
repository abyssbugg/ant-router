package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Duration
		wantErr bool
		errCode Code
	}{
		{
			name:    "valid 30s",
			input:   "30s",
			want:    Duration{30 * time.Second},
			wantErr: false,
		},
		{
			name:    "valid 1m",
			input:   "1m",
			want:    Duration{1 * time.Minute},
			wantErr: false,
		},
		{
			name:    "above maximum 2h",
			input:   "2h",
			wantErr: true,
			errCode: CodeValidationFailed,
		},
		{
			name:    "valid 1500ms",
			input:   "1500ms",
			want:    Duration{1500 * time.Millisecond},
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
			errCode: CodeValidationFailed,
		},
		{
			name:    "invalid format",
			input:   "invalid",
			wantErr: true,
			errCode: CodeValidationFailed,
		},
		{
			name:    "zero duration",
			input:   "0s",
			wantErr: true,
			errCode: CodeValidationFailed,
		},
		{
			name:    "negative duration",
			input:   "-5s",
			wantErr: true,
			errCode: CodeValidationFailed,
		},
		{
			name:    "below minimum 1s",
			input:   "500ms",
			wantErr: true,
			errCode: CodeValidationFailed,
		},
		{
			name:    "above maximum 10m",
			input:   "11m",
			wantErr: true,
			errCode: CodeValidationFailed,
		},
		{
			name:    "valid minimum boundary 1s",
			input:   "1s",
			want:    Duration{1 * time.Second},
			wantErr: false,
		},
		{
			name:    "valid maximum boundary 10m",
			input:   "10m",
			want:    Duration{10 * time.Minute},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDuration() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errCode != "" {
				code, ok := CodeOf(err)
				if !ok {
					t.Errorf("ParseDuration() error code not extractable: %v", err)
					return
				}
				if code != tt.errCode {
					t.Errorf("ParseDuration() error code = %v, want %v", code, tt.errCode)
				}
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDurationString(t *testing.T) {
	tests := []struct {
		d    Duration
		want string
	}{
		{Duration{30 * time.Second}, "30s"},
		{Duration{1 * time.Minute}, "1m0s"},
		{Duration{1500 * time.Millisecond}, "1.5s"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.d.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDurationMarshalText(t *testing.T) {
	d := Duration{30 * time.Second}
	data, err := d.MarshalText()
	if err != nil {
		t.Errorf("MarshalText() error = %v", err)
		return
	}
	if got := string(data); got != "30s" {
		t.Errorf("MarshalText() = %q, want %q", got, "30s")
	}
}

func TestDurationUnmarshalText(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    Duration
		wantErr bool
	}{
		{
			name:    "valid 30s",
			input:   []byte("30s"),
			want:    Duration{30 * time.Second},
			wantErr: false,
		},
		{
			name:    "valid 1m",
			input:   []byte("1m"),
			want:    Duration{1 * time.Minute},
			wantErr: false,
		},
		{
			name:    "empty bytes",
			input:   []byte(""),
			wantErr: true,
		},
		{
			name:    "invalid format",
			input:   []byte("invalid"),
			wantErr: true,
		},
		{
			name:    "zero duration",
			input:   []byte("0s"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := d.UnmarshalText(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && d != tt.want {
				t.Errorf("UnmarshalText() = %v, want %v", d, tt.want)
			}
		})
	}
}

func TestDurationTextRoundTrip(t *testing.T) {
	original := Duration{30 * time.Second}
	data, err := original.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}

	var restored Duration
	err = restored.UnmarshalText(data)
	if err != nil {
		t.Fatalf("UnmarshalText() error = %v", err)
	}

	if original != restored {
		t.Errorf("Round-trip failed: original %v, restored %v", original, restored)
	}
}

func TestDurationErrorsIs(t *testing.T) {
	_, err := ParseDuration("0s")
	if err == nil {
		t.Fatal("Expected error for zero duration")
	}

	var configErr *Error
	if !errors.As(err, &configErr) {
		t.Error("Error should be *Error type")
	}
}

func TestDurationErrorCodeLowercase(t *testing.T) {
	// Error code must be lowercase
	_, err := ParseDuration("invalid")
	if err == nil {
		t.Fatal("Expected error for invalid duration")
	}

	code, ok := CodeOf(err)
	if !ok {
		t.Fatal("Expected to extract code from error")
	}

	if code != "validation_failed" {
		t.Errorf("Error code = %v, want 'validation_failed'", code)
	}

	// Verify in error message
	errMsg := err.Error()
	if !strings.Contains(errMsg, "validation_failed") {
		t.Errorf("Error message should contain lowercase code: %v", errMsg)
	}
}

func TestDurationParseErrorSafety(t *testing.T) {
	// Parse errors should not include raw input for secrets
	secretLike := "my-secret-token-value"
	_, err := ParseDuration(secretLike)
	if err == nil {
		t.Fatal("Expected error for invalid duration")
	}

	errMsg := err.Error()
	// Should not contain the raw malformed input
	if strings.Contains(errMsg, secretLike) {
		t.Errorf("Parse error should not contain raw input: %v", errMsg)
	}
}

func TestDurationTextRoundTripMultiple(t *testing.T) {
	// Test roundtrip for 1s, 30s, and 10m
	tests := []Duration{
		{1 * time.Second},
		{30 * time.Second},
		{10 * time.Minute},
	}

	for _, original := range tests {
		t.Run(original.String(), func(t *testing.T) {
			data, err := original.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText() error = %v", err)
			}

			var restored Duration
			err = restored.UnmarshalText(data)
			if err != nil {
				t.Fatalf("UnmarshalText() error = %v", err)
			}

			if original != restored {
				t.Errorf("Round-trip failed: original %v, restored %v", original, restored)
			}
		})
	}
}
