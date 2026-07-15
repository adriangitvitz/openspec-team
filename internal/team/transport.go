package team

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

// postJSON posts JSON with bounded retries on transient failures (network errors, 429, 5xx); err is non-nil only when every attempt failed.
func postJSON(client *http.Client, retryDelay time.Duration, url string, headers map[string]string, payload []byte) (int, []byte, http.Header, error) {
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(retryDelay * time.Duration(attempt-1))
		}
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return 0, nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateForError(body))
			continue
		}
		return resp.StatusCode, body, resp.Header, nil
	}
	return 0, nil, nil, fmt.Errorf("giving up after %d attempts: %w", maxAttempts, lastErr)
}
