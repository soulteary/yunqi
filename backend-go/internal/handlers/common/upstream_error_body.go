package common

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/soulteary/yunqi/internal/utils"
)

// Error responses are finite diagnostic documents, even for an SSE request.
// Bound both encoded and decoded bytes; a healthy 2xx stream never uses this reader.
const maxUpstreamErrorBodyBytes = MaxUpstreamResponseLogBytes

func readUpstreamErrorBody(ctx context.Context, resp *http.Response, timeout time.Duration) ([]byte, error) {
	readCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body := resp.Body
	defer body.Close()
	// Closing the HTTP body interrupts a pending Read; no detached reader goroutine survives.
	stop := context.AfterFunc(readCtx, func() { _ = body.Close() })
	defer stop()
	data, err := io.ReadAll(io.LimitReader(body, maxUpstreamErrorBodyBytes+1))
	tooLarge := len(data) > maxUpstreamErrorBodyBytes
	if tooLarge {
		data = data[:maxUpstreamErrorBodyBytes]
	}
	if readCtx.Err() != nil {
		return data, readCtx.Err()
	}
	if tooLarge {
		return data, fmt.Errorf("upstream error body exceeds %d bytes", maxUpstreamErrorBodyBytes)
	}
	if err != nil {
		return data, err
	}
	reader, err := utils.NewDecompressedReader(resp, io.NopCloser(bytes.NewReader(data)))
	if err != nil {
		return data, err
	}
	defer reader.Close()
	decoded, err := io.ReadAll(io.LimitReader(reader, maxUpstreamErrorBodyBytes+1))
	if len(decoded) > maxUpstreamErrorBodyBytes {
		return decoded[:maxUpstreamErrorBodyBytes], fmt.Errorf("decoded upstream error body exceeds %d bytes", maxUpstreamErrorBodyBytes)
	}
	if err != nil {
		return data, err // Preserve the previous raw-body fallback for invalid compression.
	}
	return decoded, nil
}
