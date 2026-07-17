package main

import (
	"bufio"
	"errors"
	"io"
)

const defaultParseMaxLineBytes = 500 << 20

type jsonlScanner struct {
	reader  *bufio.Reader
	maxLine int
	line    []byte
	err     error
	done    bool
	skipped int
}

func newJSONLScanner(r io.Reader, maxLineBytes int) *jsonlScanner {
	return &jsonlScanner{
		reader:  bufio.NewReaderSize(r, 64*1024),
		maxLine: maxLineBytes,
		line:    make([]byte, 0, min(maxLineBytes, 64*1024)),
	}
}

// Scan skips an oversized JSONL event and continues with the next event.
func (s *jsonlScanner) Scan() bool {
	if s.done || s.err != nil || s.maxLine <= 0 {
		return false
	}

nextLine:
	s.line = s.line[:0]
	oversized := false
	for {
		fragment, err := s.reader.ReadSlice('\n')
		if !oversized {
			if len(s.line)+len(fragment) > s.maxLine {
				s.line = s.line[:0]
				oversized = true
			} else {
				s.line = append(s.line, fragment...)
			}
		}

		switch {
		case err == nil:
			if oversized {
				s.skipped++
				goto nextLine
			}
			return true
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			s.done = true
			if oversized {
				s.skipped++
				return false
			}
			return len(s.line) > 0
		default:
			s.err = err
			return false
		}
	}
}

func (s *jsonlScanner) Bytes() []byte { return s.line }
func (s *jsonlScanner) Err() error    { return s.err }
func (s *jsonlScanner) Skipped() int  { return s.skipped }
