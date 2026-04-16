package http

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"strings"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-Id")
		if rid == "" {
			rid = uuid.NewString()
		}

		w.Header().Set("X-Request-Id", rid)
		ctx := context.WithValue(r.Context(), requestIDKey, rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetRequestID(r *http.Request) string {
	if v := r.Context().Value(requestIDKey); v != nil {
		if rid, ok := v.(string); ok {
			return rid
		}
	}
	return ""
}

type statusWriter struct {
	http.ResponseWriter
	status       int
	bytesWritten int
}

func (sw *statusWriter) WriteHeader(status int) {
	sw.status = status
	sw.ResponseWriter.WriteHeader(status)
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	n, err := sw.ResponseWriter.Write(b)
	sw.bytesWritten += n
	return n, err
}

func (sw *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := sw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

func (sw *statusWriter) Flush() {
	if f, ok := sw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (sw *statusWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := sw.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (sw *statusWriter) Unwrap() http.ResponseWriter {
	return sw.ResponseWriter
}

var (
	// HTTP
	httpRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests partitioned by method, route, and status code.",
		},
		[]string{"method", "route", "status"},
	)

	httpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route"},
	)

	// WebSocket
	ActiveWSConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_active_connections",
		Help: "Number of currently active WebSocket connections.",
	})

	WSMessagesReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_messages_received_total",
			Help: "Total WebSocket messages received from clients, partitioned by message type.",
		},
		[]string{"type"},
	)

	WSRateLimitRejections = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_rate_limit_rejections_total",
		Help: "Total WebSocket connections dropped because the per-connection rate limit was exceeded.",
	})

	// Messaging
	MessagesSent = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "messages_sent_total",
		Help: "Total number of chat messages successfully stored in the database.",
	})

	MessageDeduplicated = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "messages_deduplicated_total",
		Help: "Total number of duplicate messages suppressed via idempotency key.",
	})

	MessagePublishFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "messages_publish_failures_total",
		Help: "Total number of Redis publish failures for message.created events.",
	})

	// Database
	DBQueryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "db_query_duration_seconds",
			Help:    "Latency of database queries partitioned by operation name.",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
		},
		[]string{"operation"},
	)

	// Rate limiting (HTTP layer)
	HTTPRateLimitRejections = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_rate_limit_rejections_total",
			Help: "Total HTTP requests rejected due to rate limiting, partitioned by limiter type.",
		},
		[]string{"limiter"},
	)

	// Redis
	RedisOpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "redis_op_duration_seconds",
			Help:    "Latency of Redis operations partitioned by operation name.",
			Buckets: []float64{.0001, .0005, .001, .0025, .005, .01, .025, .05, .1},
		},
		[]string{"operation"},
	)

	initHTTPMetricsOnce sync.Once
)

// InitHTTPMetrics registers all custom Prometheus metrics. Call once at startup.
func InitHTTPMetrics() {
	initHTTPMetricsOnce.Do(func() {
		prometheus.MustRegister(
			httpRequests,
			httpDuration,
			ActiveWSConnections,
			WSMessagesReceived,
			WSRateLimitRejections,
			MessagesSent,
			MessageDeduplicated,
			MessagePublishFailures,
			DBQueryDuration,
			HTTPRateLimitRejections,
			RedisOpDuration,
		)
	})
}

// ObserveDBQuery times a database operation. Use as a deferred call:
//
//	defer ObserveDBQuery("message_create", time.Now())
func ObserveDBQuery(operation string, start time.Time) {
	DBQueryDuration.WithLabelValues(operation).Observe(time.Since(start).Seconds())
}

// ObserveRedisOp times a Redis operation. Use as a deferred call:
//
//	defer ObserveRedisOp("token_get", time.Now())
func ObserveRedisOp(operation string, start time.Time) {
	RedisOpDuration.WithLabelValues(operation).Observe(time.Since(start).Seconds())
}

func routeTemplate(r *http.Request) string {
	if rt := mux.CurrentRoute(r); rt != nil {
		if tpl, err := rt.GetPathTemplate(); err == nil && tpl != "" {
			return tpl
		}
	}
	return "unknown"
}

func AccessLogAndMetrics(log *zap.Logger) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{
				ResponseWriter: w,
				status:         http.StatusOK,
			}

			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic_recovered",
						zap.String("request_id", GetRequestID(r)),
						zap.String("method", r.Method),
						zap.String("route", routeTemplate(r)),
						zap.String("path", r.URL.Path),
						zap.String("query", r.URL.RawQuery),
						zap.String("client_ip", clientIP(r)),
						zap.Any("panic", rec),
					)

					if sw.bytesWritten == 0 {
						writeJSONError(sw, http.StatusInternalServerError, "internal server error")
					}
				}

				dur := time.Since(start)
				route := routeTemplate(r)
				statusStr := strconv.Itoa(sw.status)

				httpRequests.WithLabelValues(r.Method, route, statusStr).Inc()
				httpDuration.WithLabelValues(r.Method, route).Observe(dur.Seconds())

				if sw.status == http.StatusTooManyRequests {
					HTTPRateLimitRejections.WithLabelValues("http").Inc()
				}

				fields := []zap.Field{
					zap.String("request_id", GetRequestID(r)),
					zap.String("method", r.Method),
					zap.String("route", route),
					zap.String("path", r.URL.Path),
					zap.String("query", r.URL.RawQuery),
					zap.Int("status", sw.status),
					zap.Int64("duration_ms", dur.Milliseconds()),
					zap.Duration("duration", dur),
					zap.Int("bytes_written", sw.bytesWritten),
					zap.String("remote_addr", r.RemoteAddr),
					zap.String("client_ip", clientIP(r)),
					zap.String("host", r.Host),
					zap.String("proto", r.Proto),
					zap.String("scheme", requestScheme(r)),
					zap.String("user_agent", r.UserAgent()),
					zap.String("referer", r.Referer()),
					zap.String("origin", r.Header.Get("Origin")),
					zap.String("content_type", r.Header.Get("Content-Type")),
					zap.Int64("content_length", r.ContentLength),
				}

				if user := CurrentUser(r); user != nil {
					fields = append(fields,
						zap.Int64("user_id", user.ID),
						zap.String("username", user.Username),
					)
				}

				switch {
				case sw.status >= 500:
					log.Error("http_request", fields...)
				case sw.status >= 400:
					log.Warn("http_request", fields...)
				default:
					log.Info("http_request", fields...)
				}
			}()

			next.ServeHTTP(sw, r)
		})
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	if xrip := r.Header.Get("X-Real-Ip"); xrip != "" {
		return strings.TrimSpace(xrip)
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func requestScheme(r *http.Request) string {
	if xfp := r.Header.Get("X-Forwarded-Proto"); xfp != "" {
		return xfp
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
