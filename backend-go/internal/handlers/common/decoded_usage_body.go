package common

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/soulteary/yunqi/internal/utils"
)

// Do not read compression headers before the protocol handler starts its
// first-content watchdog. Capture still sees decoded bytes; subsequent handler
// decompression is idempotent because the encoding metadata is cleared here.
func prepareDecodedUsageResponse(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	encoding := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Encoding"), ",")[0]))
	if encoding == "" || encoding == "identity" {
		return nil
	}
	if encoding != "gzip" && encoding != "deflate" && encoding != "zstd" {
		return utils.DecompressResponseBodyIfNeeded(resp)
	}
	resp.Body = &decodedUsageBody{raw: &onceUsageBody{ReadCloser: resp.Body}, encoding: encoding}
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	resp.Uncompressed = true
	return nil
}

type onceUsageBody struct {
	io.ReadCloser
	once sync.Once
	err  error
}

func (b *onceUsageBody) Close() error {
	b.once.Do(func() { b.err = b.ReadCloser.Close() })
	return b.err
}

type decodedUsageBody struct {
	raw         *onceUsageBody
	encoding    string
	readMu      sync.Mutex
	initialized bool
	reader      io.ReadCloser
	initErr     error
	closed      atomic.Bool
	closeOnce   sync.Once
	closeErr    error
}

func (b *decodedUsageBody) Read(p []byte) (int, error) {
	b.readMu.Lock()
	defer b.readMu.Unlock()
	if b.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	if !b.initialized {
		b.initialized = true
		b.reader, b.initErr = utils.NewDecompressedReaderByEncoding(b.encoding, b.raw)
	}
	if b.initErr != nil {
		return 0, compressionReadError(b.initErr)
	}
	n, err := b.reader.Read(p)
	return n, compressionReadError(err)
}
func compressionReadError(err error) error {
	if err == nil || errors.Is(err, io.EOF) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrInvalidResponseBody, err)
}
func (b *decodedUsageBody) Close() error {
	b.closeOnce.Do(func() {
		b.closed.Store(true)
		// Interrupt a blocked decoder header/read before taking the read lock.
		b.closeErr = b.raw.Close()
		b.readMu.Lock()
		defer b.readMu.Unlock()
		if b.reader != nil {
			if err := b.reader.Close(); b.closeErr == nil {
				b.closeErr = err
			}
		}
	})
	return b.closeErr
}
