package apmgologger

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/savsgio/go-logger/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.elastic.co/apm/v2"
	"go.elastic.co/apm/v2/transport/transporttest"
)

func newLogger(w io.Writer, fields ...logger.Field) *logger.Logger {
	l := logger.New(logger.DEBUG, w, fields...)
	l.SetEncoder(logger.NewEncoderJSON(logger.EncoderJSONConfig{}))

	return l
}

func makeError() error {
	return errors.New("kablamo")
}

func TestHook(t *testing.T) {
	tracer, transport := transporttest.NewRecorderTracer()
	defer tracer.Close()

	var buf bytes.Buffer

	fields := []logger.Field{
		{Key: "foo", Value: "bar"},
	}

	log := newLogger(&buf, fields...)
	if err := log.AddHook(&Hook{Tracer: tracer}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	log.Errorf("¡hola, %s!", "mundo")

	assert.Regexp(t, `{"datetime":"(.*)","level":"ERROR","foo":"bar","message":"¡hola, mundo!"}`+"\n", buf.String())

	tracer.Flush(nil)

	payloads := transport.Payloads()
	assert.Len(t, payloads.Errors, 1)

	err0 := payloads.Errors[0]
	assert.Equal(t, "¡hola, mundo!", err0.Log.Message)
	assert.Equal(t, "ERROR", err0.Log.Level)
	assert.Equal(t, "", err0.Log.LoggerName)
	assert.Equal(t, "", err0.Log.ParamMessage)
	assert.Equal(t, "TestHook", err0.Culprit)
	assert.NotEmpty(t, err0.Log.Stacktrace)
	assert.False(t, time.Time(err0.Timestamp).IsZero())
	assert.Zero(t, err0.ParentID)
	assert.Zero(t, err0.TraceID)
	assert.Zero(t, err0.TransactionID)

	assert.NotNil(t, err0.Context)

	ctxCustomFields := []logger.Field{}

	for _, kv := range err0.Context.Custom {
		assert.Regexp(t, "^log_fields_(.*)$", kv.Key)

		fieldKey := strings.Split(kv.Key, "_")[2]
		ctxCustomFields = append(ctxCustomFields, logger.Field{Key: fieldKey, Value: kv.Value})
	}

	assert.Equal(t, ctxCustomFields, fields)
}

func TestHookTransactionTraceContext(t *testing.T) {
	tracer, transport := transporttest.NewRecorderTracer()
	defer tracer.Close()

	log := newLogger(ioutil.Discard)
	if err := log.AddHook(&Hook{Tracer: tracer}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tx := tracer.StartTransaction("name", "type")
	ctx := apm.ContextWithTransaction(context.Background(), tx)
	span, ctx := apm.StartSpan(ctx, "name", "type")

	log.WithFields(TraceContext(ctx)...).Errorf("¡hola, %s!", "mundo")
	span.End()
	tx.End()

	tracer.Flush(nil)

	payloads := transport.Payloads()
	assert.Len(t, payloads.Transactions, 1)
	assert.Len(t, payloads.Spans, 1)
	assert.Len(t, payloads.Errors, 1)

	err0 := payloads.Errors[0]
	assert.Equal(t, payloads.Spans[0].ID, err0.ParentID)
	assert.Equal(t, payloads.Transactions[0].TraceID, err0.TraceID)
	assert.Equal(t, payloads.Transactions[0].ID, err0.TransactionID)
}

func TestHookWithError(t *testing.T) {
	tracer, transport := transporttest.NewRecorderTracer()
	defer tracer.Close()

	log := newLogger(ioutil.Discard)
	if err := log.AddHook(&Hook{Tracer: tracer}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	someErr := makeError()

	log.Errorf("nope: %s", someErr)

	tracer.Flush(nil)

	payloads := transport.Payloads()
	assert.Len(t, payloads.Errors, 1)

	err0 := payloads.Errors[0]
	assert.Equal(t, "kablamo", err0.Exception.Message)
	assert.Equal(t, fmt.Sprintf("nope: %v", someErr), err0.Log.Message)
	assert.Equal(t, "makeError", err0.Culprit)
	assert.NotEmpty(t, err0.Log.Stacktrace)
	assert.NotEmpty(t, err0.Exception.Stacktrace)
	assert.NotEqual(t, err0.Log.Stacktrace, err0.Exception.Stacktrace)
	assert.Equal(t, "makeError", err0.Exception.Stacktrace[0].Function)
	assert.Equal(t, "(*Hook).Fire", err0.Log.Stacktrace[0].Function)
}

func TestHookFatal(t *testing.T) {
	if os.Getenv("_INSIDE_TEST") == "1" {
		tracer, _ := apm.NewTracer("", "")

		log := logger.New(logger.INFO, os.Stderr)
		if err := log.AddHook(&Hook{Tracer: tracer}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		log.Fatal("fatality!")
	}

	var recorder transporttest.RecorderTransport

	mux := http.NewServeMux()
	mux.HandleFunc("/intake/v2/events", func(w http.ResponseWriter, req *http.Request) {
		if err := recorder.SendStream(req.Context(), req.Body); err != nil {
			panic(err)
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^TestHookFatal$") // nolint:gosec
	cmd.Env = append(os.Environ(),
		"_INSIDE_TEST=1",
		"ELASTIC_APM_SERVER_URL="+server.URL,
		"ELASTIC_APM_LOG_FILE=stderr",
		"ELASTIC_APM_LOG_LEVEL=debug",
	)

	output, err := cmd.CombinedOutput()
	require.Error(t, err)

	defer func() {
		if t.Failed() {
			t.Logf("%s", output)
		}
	}()

	payloads := recorder.Payloads()
	require.Len(t, payloads.Errors, 1)
	assert.Equal(t, "fatality!", payloads.Errors[0].Log.Message)
	assert.Equal(t, "FATAL", payloads.Errors[0].Log.Level)
}

func TestHookTracerClosed(t *testing.T) {
	tracer, _ := transporttest.NewRecorderTracer()
	tracer.Close() // close it straight away, hook should return immediately

	log := newLogger(ioutil.Discard)
	if err := log.AddHook(&Hook{Tracer: tracer}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	log.Error("boom")
}
