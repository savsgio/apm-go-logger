package apmgologger

import (
	"context"

	"github.com/savsgio/go-logger/v4"
	"go.elastic.co/apm/v2"
)

const (
	// FieldKeyTraceID is the field key for the trace ID.
	FieldKeyTraceID = "trace.id"

	// FieldKeyTransactionID is the field key for the transaction ID.
	FieldKeyTransactionID = "transaction.id"

	// FieldKeySpanID is the field key for the span ID.
	FieldKeySpanID = "span.id"
)

// TraceContext returns a []logger.Field containing the trace
// context of the transaction and span contained in ctx, if any.
func TraceContext(ctx context.Context) []logger.Field {
	tx := apm.TransactionFromContext(ctx)
	if tx == nil {
		return nil
	}

	traceContext := tx.TraceContext()
	fields := []logger.Field{
		{Key: FieldKeyTraceID, Value: traceContext.Trace},
		{Key: FieldKeyTransactionID, Value: traceContext.Span},
	}

	if span := apm.SpanFromContext(ctx); span != nil {
		fields = append(fields, logger.Field{
			Key: FieldKeySpanID, Value: span.TraceContext().Span,
		})
	}

	return fields
}
