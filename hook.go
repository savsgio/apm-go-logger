package apmgologger

import (
	"context"
	"time"

	"github.com/savsgio/go-logger/v4"
	"go.elastic.co/apm/v2"
	"go.elastic.co/apm/v2/stacktrace"
)

// DefaultLogLevels is the log levels for which errors are reported by Hook, if Hook.LogLevels is not set.
var DefaultLogLevels = []logger.Level{
	logger.PANIC,
	logger.FATAL,
	logger.ERROR,
}

const (
	// DefaultFatalFlushTimeout is the default value for Hook.FatalFlushTimeout.
	DefaultFatalFlushTimeout = 5 * time.Second
)

func init() { // nolint:gochecknoinits
	stacktrace.RegisterLibraryPackage("github.com/savsgio/go-logger")
}

// Hook implements logger.Hook, reporting log records as errors
// to the APM Server. If TraceContext is used to add trace IDs
// to the log records, the errors reported will be associated
// with them.
type Hook struct {
	// Tracer is the apm.Tracer to use for reporting errors.
	// If Tracer is nil, then apm.DefaultTracer() will be used.
	Tracer *apm.Tracer

	// LogLevels holds the log levels to report as errors.
	// If LogLevels is nil, then the DefaultLogLevels will
	// be used.
	LogLevels []logger.Level

	// FatalFlushTimeout is the amount of time to wait while
	// flushing a fatal log message to the APM Server before
	// the process is exited. If this is 0, then
	// DefaultFatalFlushTimeout will be used. If the timeout
	// is a negative value, then no flushing will be performed.
	FatalFlushTimeout time.Duration
}

func (h *Hook) tracer() *apm.Tracer {
	tracer := h.Tracer
	if tracer == nil {
		tracer = apm.DefaultTracer()
	}

	return tracer
}

// Levels returns h.LogLevels, satisfying the logger.Hook interface.
func (h *Hook) Levels() []logger.Level {
	if h.LogLevels != nil {
		return h.LogLevels
	}

	return DefaultLogLevels
}

func (h *Hook) getFieldValue(fields []logger.Field, key string) interface{} {
	for i := range fields {
		if field := fields[i]; field.Key == key {
			return field.Value
		}
	}

	return nil
}

func (h *Hook) getError(args []interface{}) error {
	for i := range args {
		if err, ok := args[i].(error); ok {
			return err
		}
	}

	return nil
}

// Fire reports the log entry as an error to the APM Server.
func (h *Hook) Fire(entry logger.Entry) error {
	tracer := h.tracer()
	if !tracer.Recording() {
		return nil
	}

	err := h.getError(entry.Args)
	errlog := tracer.NewErrorLog(apm.ErrorLogRecord{
		Message: entry.Message,
		Level:   entry.Level.String(),
		Error:   err,
	})
	errlog.Handled = true
	errlog.Timestamp = entry.Time

	stacktrace := 1
	if err == nil {
		stacktrace++
	}

	errlog.SetStacktrace(stacktrace)

	// Extract trace context added with apmgologger.TraceContext,
	// and include it in the reported error.
	if traceID, ok := h.getFieldValue(entry.Config.Fields, FieldKeyTraceID).(apm.TraceID); ok {
		errlog.TraceID = traceID
	}

	if transactionID, ok := h.getFieldValue(entry.Config.Fields, FieldKeyTransactionID).(apm.SpanID); ok {
		errlog.TransactionID = transactionID
		errlog.ParentID = transactionID
	}

	if spanID, ok := h.getFieldValue(entry.Config.Fields, FieldKeySpanID).(apm.SpanID); ok {
		errlog.ParentID = spanID
	}

	errlog.Send()

	if entry.Level == logger.FATAL {
		// In its default configuration, logger will exit the process
		// following a fatal log message, so we flush the tracer.
		flushTimeout := h.FatalFlushTimeout
		if flushTimeout == 0 {
			flushTimeout = DefaultFatalFlushTimeout
		}

		if flushTimeout >= 0 {
			ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
			defer cancel()

			tracer.Flush(ctx.Done())
		}
	}

	return nil
}
