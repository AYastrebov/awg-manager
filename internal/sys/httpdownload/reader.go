// Package httpdownload provides shared utilities for streaming HTTP
// downloads with throttled progress reporting. Used by both the
// hydraroute geo-data store and the singbox installer to surface
// live byte-stream progress over SSE without each caller maintaining
// its own throttled counter.
package httpdownload

import (
	"io"
	"time"
)

// emitInterval throttles progress callbacks. 64 KB or 200 ms (whichever
// fires first) keeps SSE traffic sane during a download — at full router
// uplink speeds (~5 MB/s) this caps notifications around ~5 / s.
const (
	emitBytes    = 64 * 1024
	emitDuration = 200 * time.Millisecond
)

// ProgressFn receives cumulative bytes downloaded and the expected total
// (0 if Content-Length was absent). Called from the goroutine driving
// the HTTP body — must not block.
type ProgressFn func(downloaded, total int64)

// Reader wraps an io.Reader and calls onProgress periodically with the
// cumulative bytes read so far. Throttled to emit at most every ~64 KB
// or 200 ms (whichever comes first), plus once on EOF so callers can
// render a final "100%" frame deterministically.
type Reader struct {
	r          io.Reader
	total      int64
	read       int64
	lastEmit   int64
	lastTime   time.Time
	onProgress ProgressFn
}

// NewReader wraps r with throttled progress reporting. total is the
// expected size (typically resp.ContentLength); pass 0 when unknown
// — callers that want a percent UI must handle total == 0.
func NewReader(r io.Reader, total int64, onProgress ProgressFn) *Reader {
	return &Reader{
		r:          r,
		total:      total,
		lastTime:   time.Now(),
		onProgress: onProgress,
	}
}

// BytesRead returns total bytes consumed from the underlying reader.
func (p *Reader) BytesRead() int64 { return p.read }

func (p *Reader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	if n > 0 {
		p.read += int64(n)
		if p.onProgress != nil {
			shouldEmit := p.read-p.lastEmit >= emitBytes ||
				time.Since(p.lastTime) >= emitDuration
			if shouldEmit {
				p.onProgress(p.read, p.total)
				p.lastEmit = p.read
				p.lastTime = time.Now()
			}
		}
	}
	// Final flush on EOF guarantees a 100% frame even when the stream
	// signals EOF on a zero-length Read (typical for net/http bodies)
	// or when the last chunk was below the byte threshold.
	if err != nil && p.onProgress != nil && p.read != p.lastEmit {
		p.onProgress(p.read, p.total)
		p.lastEmit = p.read
	}
	return n, err
}
