package apmgologger

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/savsgio/go-logger/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.elastic.co/apm/v2"
	"go.elastic.co/apm/v2/apmtest"
)

func TestTraceContext(t *testing.T) {
	var buf bytes.Buffer

	log := newLogger(&buf)

	tx, spans, _ := apmtest.WithTransaction(func(ctx context.Context) {
		span, ctx := apm.StartSpan(ctx, "name", "type")
		defer span.End()

		log.WithFields(TraceContext(ctx)...).Debug("beep")
	})
	require.Len(t, spans, 1)

	assert.Regexp(t,
		fmt.Sprintf(
			`^{"datetime":"(.*)","level":"DEBUG","trace.id":"%x","transaction.id":"%x","span.id":"%x","message":"beep"}`+"\n$",
			tx.TraceID[:], tx.ID[:], spans[0].ID[:],
		),
		buf.String(),
	)
}

func TestTraceContextTextFormatter(t *testing.T) {
	var buf bytes.Buffer

	log := newLogger(&buf)
	log.SetEncoder(logger.NewEncoderText(logger.EncoderTextConfig{}))

	tx, spans, _ := apmtest.WithTransaction(func(ctx context.Context) {
		span, ctx := apm.StartSpan(ctx, "name", "type")
		defer span.End()

		log.WithFields(TraceContext(ctx)...).Debug("beep")
	})
	require.Len(t, spans, 1)

	assert.Regexp(t,
		fmt.Sprintf(
			`(.*) - DEBUG - trace.id=%x - transaction.id=%x - span.id=%x - beep`+"\n",
			tx.TraceID[:], tx.ID[:], spans[0].ID[:],
		),
		buf.String(),
	)
}

func TestTraceContextNoSpan(t *testing.T) {
	var buf bytes.Buffer

	log := newLogger(&buf)
	tx, _, _ := apmtest.WithTransaction(func(ctx context.Context) {
		log.WithFields(TraceContext(ctx)...).Debug("beep")
	})

	assert.Regexp(t,
		fmt.Sprintf(
			`{"datetime":"(.*)","level":"DEBUG","trace.id":"%x","transaction.id":"%x","message":"beep"}`+"\n",
			tx.TraceID[:], tx.ID[:],
		),
		buf.String(),
	)
}

func TestTraceContextEmpty(t *testing.T) {
	var buf bytes.Buffer

	log := newLogger(&buf)

	// apmgologger.TraceContext will return nil if the context does not contain a transaction.
	ctx := context.Background()
	log.WithFields(TraceContext(ctx)...).Debug("beep")
	assert.Regexp(t, `{"datetime":"(.*)","level":"DEBUG","message":"beep"}`+"\n", buf.String())
}
