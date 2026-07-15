package sessionmigration

import (
	"bytes"
	"strings"
	"testing"
)

func TestStreamChunksPreservesCompleteJSONL(t *testing.T) {
	input := []byte("{\"id\":1}\n{\"id\":2}\n{\"id\":3}")
	var output bytes.Buffer
	var chunks []chunk
	end, endLine, err := streamChunks(bytes.NewReader(input), 0, 1, func(item chunk) error {
		chunks = append(chunks, item)
		output.Write(item.Content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if end != int64(len(input)) || endLine != 3 || !bytes.Equal(output.Bytes(), input) {
		t.Fatalf("end=%d line=%d output=%q", end, endLine, output.Bytes())
	}
	if len(chunks) != 1 || chunks[0].StartLine != 1 || chunks[0].EndLine != 3 {
		t.Fatalf("chunks=%+v", chunks)
	}
}

func TestStreamChunksRejectsIncompleteTail(t *testing.T) {
	_, _, err := streamChunks(strings.NewReader("{\"id\":1}\n{\"id\":"), 0, 1, func(chunk) error { return nil })
	if err == nil {
		t.Fatal("incomplete JSONL tail was accepted")
	}
}
