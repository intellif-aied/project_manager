package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

var (
	errSessionLineTooLarge  = errors.New("session JSONL line exceeds the configured limit")
	errSessionChunkTooLarge = errors.New("session JSONL line cannot fit in a chunk")
)

type sessionChunkLimits struct {
	MaxEvents     int
	MaxChunkBytes int
	MaxLineBytes  int
}

type localSessionChunk struct {
	StartCursor   int64
	EndCursor     int64
	StartLine     int64
	EndLine       int64
	Content       []byte
	ContentSHA256 string
}

func splitSessionJSONL(
	r io.Reader,
	startCursor int64,
	startLine int64,
	limits sessionChunkLimits,
) ([]localSessionChunk, bool, error) {
	chunks := make([]localSessionChunk, 0)
	pendingTail, err := streamSessionJSONLChunks(r, startCursor, startLine, limits, func(chunk localSessionChunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	return chunks, pendingTail, err
}

func streamSessionJSONLChunks(
	r io.Reader,
	startCursor int64,
	startLine int64,
	limits sessionChunkLimits,
	emit func(localSessionChunk) error,
) (bool, error) {
	if startCursor < 0 || startLine < 1 || limits.MaxEvents <= 0 || limits.MaxChunkBytes <= 0 || limits.MaxLineBytes <= 0 {
		return false, errors.New("invalid chunking input")
	}
	if emit == nil {
		return false, errors.New("chunk emitter is required")
	}

	readerSize := limits.MaxLineBytes
	if readerSize > 64*1024 {
		readerSize = 64 * 1024
	}
	if readerSize < 4096 {
		readerSize = 4096
	}
	reader := bufio.NewReaderSize(r, readerSize)

	var chunk bytes.Buffer
	cursor := startCursor
	lineNumber := startLine
	chunkStartCursor := startCursor
	chunkStartLine := startLine
	chunkEvents := 0
	pendingTail := false

	flush := func() error {
		if chunkEvents == 0 {
			return nil
		}
		content := append([]byte(nil), chunk.Bytes()...)
		sum := sha256.Sum256(content)
		if err := emit(localSessionChunk{
			StartCursor:   chunkStartCursor,
			EndCursor:     cursor,
			StartLine:     chunkStartLine,
			EndLine:       lineNumber - 1,
			Content:       content,
			ContentSHA256: hex.EncodeToString(sum[:]),
		}); err != nil {
			return err
		}
		chunk.Reset()
		chunkEvents = 0
		chunkStartCursor = cursor
		chunkStartLine = lineNumber
		return nil
	}

	for {
		line, complete, err := readCompleteJSONLLine(reader, limits.MaxLineBytes)
		if err != nil {
			return false, err
		}
		if !complete {
			pendingTail = len(line) > 0
			break
		}
		if len(line) > limits.MaxChunkBytes {
			return false, fmt.Errorf("%w: line=%d bytes=%d limit=%d", errSessionChunkTooLarge, lineNumber, len(line), limits.MaxChunkBytes)
		}
		if chunkEvents > 0 && (chunkEvents == limits.MaxEvents || chunk.Len()+len(line) > limits.MaxChunkBytes) {
			if err := flush(); err != nil {
				return false, err
			}
		}
		if chunkEvents == 0 {
			chunkStartCursor = cursor
			chunkStartLine = lineNumber
		}
		_, _ = chunk.Write(line)
		chunkEvents++
		cursor += int64(len(line))
		lineNumber++
		if chunkEvents == limits.MaxEvents || chunk.Len() == limits.MaxChunkBytes {
			if err := flush(); err != nil {
				return false, err
			}
		}
	}
	if err := flush(); err != nil {
		return false, err
	}
	return pendingTail, nil
}

func readCompleteJSONLLine(reader *bufio.Reader, maxLineBytes int) ([]byte, bool, error) {
	line := make([]byte, 0, min(maxLineBytes, 64*1024))
	for {
		fragment, err := reader.ReadSlice('\n')
		line = append(line, fragment...)
		if len(line) > maxLineBytes {
			return nil, false, errSessionLineTooLarge
		}
		switch {
		case err == nil:
			return line, true, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return line, false, nil
		default:
			return nil, false, err
		}
	}
}
