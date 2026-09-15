package server

import (
	"errors"
	"testing"
)

func TestParseNum(t *testing.T) {
	tests := []struct {
		name     string
		rawQuery string
		want     int64
		wantErr  error
	}{
		{"simple", "num=5", 5, nil},
		{"negative", "num=-3", -3, nil},
		{"leading params", "foo=1&num=42", 42, nil},
		{"trailing params", "num=7&bar=2", 7, nil},
		{"multi-value first wins", "num=1&num=2", 1, nil},
		{"missing", "", 0, errMissingNum},
		{"missing wrong name", "n=1", 0, errMissingNum},
		{"missing wrong order", "=num&", 0, errMissingNum},
		{"empty value", "num=", 0, errBadNum},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseNum(tt.rawQuery)
			if tt.wantErr == errMissingNum || tt.wantErr == errBadNum {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("parseNum(%q) error = %v, want %v", tt.rawQuery, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseNum(%q) unexpected error: %v", tt.rawQuery, err)
			}
			if got != tt.want {
				t.Errorf("parseNum(%q) = %d, want %d", tt.rawQuery, got, tt.want)
			}
		})
	}
}

func TestParseNumNonInteger(t *testing.T) {
	if _, err := parseNum("num=abc"); err == nil {
		t.Fatal("parseNum(\"num=abc\") expected an error")
	}
}
