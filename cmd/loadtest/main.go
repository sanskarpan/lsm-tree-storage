package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type summary struct {
	Target        string        `json:"target"`
	Duration      time.Duration `json:"duration"`
	Concurrency   int           `json:"concurrency"`
	Keyspace      int           `json:"keyspace"`
	WritePercent  int           `json:"write_percent"`
	ValueSize     int           `json:"value_size"`
	TotalOps      uint64        `json:"total_ops"`
	SuccessfulOps uint64        `json:"successful_ops"`
	FailedOps     uint64        `json:"failed_ops"`
	ReadOps       uint64        `json:"read_ops"`
	WriteOps      uint64        `json:"write_ops"`
	OpsPerSecond  float64       `json:"ops_per_second"`
	P50LatencyMS  float64       `json:"p50_latency_ms"`
	P95LatencyMS  float64       `json:"p95_latency_ms"`
	P99LatencyMS  float64       `json:"p99_latency_ms"`
	ErrorSamples  []string      `json:"error_samples,omitempty"`
	StartedAt     time.Time     `json:"started_at"`
	CompletedAt   time.Time     `json:"completed_at"`
}

func main() {
	var (
		target       = flag.String("target", "http://127.0.0.1:3001", "base URL to load test")
		duration     = flag.Duration("duration", 30*time.Second, "test duration")
		concurrency  = flag.Int("concurrency", 8, "number of worker goroutines")
		keyspace     = flag.Int("keyspace", 5000, "distinct keys to target")
		writePercent = flag.Int("write-percent", 50, "percentage of write operations")
		valueSize    = flag.Int("value-size", 128, "value size in bytes for writes")
		apiToken     = flag.String("api-token", strings.TrimSpace(os.Getenv("API_TOKEN")), "backend bearer token if required")
		basicAuth    = flag.String("basic-auth", strings.TrimSpace(os.Getenv("BFF_BASIC_AUTH")), "BFF basic auth in user:password form")
		jsonOut      = flag.Bool("json", true, "print JSON summary")
	)
	flag.Parse()

	if *concurrency <= 0 || *keyspace <= 0 || *valueSize <= 0 || *writePercent < 0 || *writePercent > 100 {
		fmt.Fprintln(os.Stderr, "invalid loadtest flags")
		os.Exit(1)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	startedAt := time.Now().UTC()
	var totalOps, successfulOps, failedOps, readOps, writeOps atomic.Uint64
	var errorMu sync.Mutex
	errorSamples := make([]string, 0, 8)
	latMu := sync.Mutex{}
	latencies := make([]time.Duration, 0, *concurrency*64)

	wg := sync.WaitGroup{}
	for workerID := 0; workerID < *concurrency; workerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(id*7919)))
			value := bytes.Repeat([]byte("v"), *valueSize)
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				key := fmt.Sprintf("load-%06d", rng.Intn(*keyspace))
				isWrite := rng.Intn(100) < *writePercent
				start := time.Now()
				var err error
				if isWrite {
					err = putValue(ctx, client, *target, key, string(value), *apiToken, *basicAuth)
					writeOps.Add(1)
				} else {
					err = getValue(ctx, client, *target, key, *apiToken, *basicAuth)
					readOps.Add(1)
				}
				duration := time.Since(start)
				if err != nil && ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
					return
				}
				totalOps.Add(1)

				latMu.Lock()
				latencies = append(latencies, duration)
				latMu.Unlock()

				if err != nil {
					failedOps.Add(1)
					errorMu.Lock()
					if len(errorSamples) < cap(errorSamples) {
						errorSamples = append(errorSamples, err.Error())
					}
					errorMu.Unlock()
					continue
				}
				successfulOps.Add(1)
			}
		}(workerID)
	}
	wg.Wait()

	latMu.Lock()
	p50 := percentile(latencies, 50)
	p95 := percentile(latencies, 95)
	p99 := percentile(latencies, 99)
	latMu.Unlock()

	completedAt := time.Now().UTC()
	elapsed := completedAt.Sub(startedAt)
	report := summary{
		Target:        *target,
		Duration:      elapsed,
		Concurrency:   *concurrency,
		Keyspace:      *keyspace,
		WritePercent:  *writePercent,
		ValueSize:     *valueSize,
		TotalOps:      totalOps.Load(),
		SuccessfulOps: successfulOps.Load(),
		FailedOps:     failedOps.Load(),
		ReadOps:       readOps.Load(),
		WriteOps:      writeOps.Load(),
		OpsPerSecond:  float64(totalOps.Load()) / elapsed.Seconds(),
		P50LatencyMS:  float64(p50.Microseconds()) / 1000,
		P95LatencyMS:  float64(p95.Microseconds()) / 1000,
		P99LatencyMS:  float64(p99.Microseconds()) / 1000,
		ErrorSamples:  errorSamples,
		StartedAt:     startedAt,
		CompletedAt:   completedAt,
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
		return
	}

	fmt.Printf("ops=%d ok=%d failed=%d ops/s=%.2f p50=%.2fms p95=%.2fms p99=%.2fms\n",
		report.TotalOps, report.SuccessfulOps, report.FailedOps, report.OpsPerSecond,
		report.P50LatencyMS, report.P95LatencyMS, report.P99LatencyMS,
	)
}

func putValue(ctx context.Context, client *http.Client, target, key, value, apiToken, basicAuth string) error {
	body, _ := json.Marshal(map[string]string{"key": key, "value": value})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(target, "/")+"/db/put", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	applyAuth(req, apiToken, basicAuth)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("put status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func getValue(ctx context.Context, client *http.Client, target, key, apiToken, basicAuth string) error {
	endpoint := strings.TrimRight(target, "/") + "/db/get?key=" + url.QueryEscape(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	applyAuth(req, apiToken, basicAuth)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("get status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func applyAuth(req *http.Request, apiToken, basicAuth string) {
	if strings.TrimSpace(apiToken) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiToken))
	}
	if strings.TrimSpace(basicAuth) != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(basicAuth))))
	}
}

func percentile(samples []time.Duration, pct int) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	cp := make([]time.Duration, len(samples))
	copy(cp, samples)
	for i := 1; i < len(cp); i++ {
		for j := i; j > 0 && cp[j-1] > cp[j]; j-- {
			cp[j-1], cp[j] = cp[j], cp[j-1]
		}
	}
	index := (len(cp)*pct + 99) / 100
	if index <= 0 {
		index = 1
	}
	if index > len(cp) {
		index = len(cp)
	}
	return cp[index-1]
}
