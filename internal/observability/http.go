package observability

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

func (r *responseRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (r *responseRecorder) Push(target string, opts *http.PushOptions) error {
	pusher, ok := r.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (r *responseRecorder) ReadFrom(src io.Reader) (int64, error) {
	readerFrom, ok := r.ResponseWriter.(io.ReaderFrom)
	if !ok {
		return io.Copy(r.ResponseWriter, src)
	}
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := readerFrom.ReadFrom(src)
	r.bytes += int(n)
	return n, err
}

// Middleware applies request IDs, access logs, and HTTP metrics.
func Middleware(logger *slog.Logger, metrics *Metrics, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)

		rec := &responseRecorder{ResponseWriter: w}
		start := time.Now()
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}

		duration := time.Since(start)
		route := normalizeRoute(r.URL.Path)
		if metrics != nil {
			metrics.ObserveHTTPRequest(r.Method, route, rec.status, duration)
		}

		logger.Info("http_request",
			slog.String("request_id", requestID),
			slog.String("method", r.Method),
			slog.String("route", route),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Int("bytes", rec.bytes),
			slog.Duration("duration", duration),
			slog.String("remote_addr", clientAddr(r)),
			slog.String("user_agent", r.UserAgent()),
		)
	})
}

// LegacyLogBridge adapts standard-library log output into structured slog events.
type LegacyLogBridge struct {
	logger *slog.Logger
	level  slog.Level
}

func NewLegacyLogBridge(logger *slog.Logger, level slog.Level) io.Writer {
	if logger == nil {
		logger = slog.Default()
	}
	return &LegacyLogBridge{logger: logger, level: level}
}

func (w *LegacyLogBridge) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg == "" {
		return len(p), nil
	}
	w.logger.Log(nil, w.level, "legacy_log", slog.String("message", msg))
	return len(p), nil
}

func normalizeRoute(path string) string {
	if path == "" {
		return "/"
	}
	if strings.HasPrefix(path, "/api/v1/") {
		path = strings.TrimPrefix(path, "/api/v1")
	}
	switch {
	case strings.HasPrefix(path, "/bloom/"):
		return "/bloom/:fileID"
	case strings.HasPrefix(path, "/scenarios/") && strings.HasSuffix(path, "/run"):
		return "/scenarios/:name/run"
	case strings.HasPrefix(path, "/assets/"):
		return "/assets/*"
	default:
		return path
	}
}

func clientAddr(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf[:])
}
