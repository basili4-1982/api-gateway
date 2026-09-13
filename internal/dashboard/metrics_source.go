package dashboard

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultScrapeTimeout = 3 * time.Second
	maxMetricsBodyBytes  = 4 << 20
)

// MetricsSummary is a snapshot of the gateway's expvar /metrics output.
type MetricsSummary struct {
	Loaded            bool             `json:"loaded"`
	ScrapedAt         time.Time        `json:"scraped_at"`
	ActiveRequests    int64            `json:"active_requests"`
	RateLimitDenials  int64            `json:"rate_limit_denials_total"`
	RequestsTotal     map[string]int64 `json:"requests_total"`
	RequestDurationMS map[string]int64 `json:"request_duration_ms"`
}

// ScrapeMetrics fetches and parses the gateway /metrics expvar text with a
// short timeout.
func ScrapeMetrics(rawURL string) (*MetricsSummary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultScrapeTimeout)
	defer cancel()
	return scrapeMetrics(ctx, http.DefaultClient, rawURL)
}

func scrapeMetrics(ctx context.Context, client *http.Client, rawURL string) (*MetricsSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("metrics request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scrape metrics: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scrape metrics: unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetricsBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read metrics: %w", err)
	}
	return parseMetrics(body)
}

// parseMetrics decodes the expvar text emitted by internal/proxy.Metrics.Handler.
// Each line is "<name> <value>", where a map value is a JSON object.
func parseMetrics(data []byte) (*MetricsSummary, error) {
	m := &MetricsSummary{
		Loaded:            true,
		ScrapedAt:         time.Now(),
		RequestsTotal:     map[string]int64{},
		RequestDurationMS: map[string]int64{},
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), maxMetricsBodyBytes)
	recognized := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "gateway_active_requests":
			recognized = true
			m.ActiveRequests = parseInt(value)
		case "gateway_rate_limit_denials_total":
			recognized = true
			m.RateLimitDenials = parseInt(value)
		case "gateway_requests_total":
			recognized = true
			m.RequestsTotal = parseMap(value)
		case "gateway_request_duration_ms":
			recognized = true
			m.RequestDurationMS = parseMap(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse metrics: %w", err)
	}
	if !recognized {
		return nil, fmt.Errorf("parse metrics: unrecognized payload")
	}
	return m, nil
}

func parseInt(v string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func parseMap(v string) map[string]int64 {
	out := map[string]int64{}
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return map[string]int64{}
	}
	return out
}
