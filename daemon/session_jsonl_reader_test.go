package main

import (
	"strings"
	"testing"
)

func TestReadJSONLLinesSkipsOversizedLineAndContinues(t *testing.T) {
	input := "{\"id\":1}\n" + strings.Repeat("x", 32) + "\n{\"id\":2}\n"
	var lines []string
	scanner := newJSONLScanner(strings.NewReader(input), 16)
	for scanner.Scan() {
		lines = append(lines, strings.TrimSpace(string(scanner.Bytes())))
	}
	if scanner.Err() != nil {
		t.Fatal(scanner.Err())
	}
	if scanner.Skipped() != 1 || len(lines) != 2 || lines[1] != `{"id":2}` {
		t.Fatalf("skipped=%d lines=%q", scanner.Skipped(), lines)
	}
}
