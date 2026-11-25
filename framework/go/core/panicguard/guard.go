package panicguard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"runtime/debug"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/core/fault"
)

type Incident struct {
	Boundary    string
	Fingerprint string
	Stack       []byte
}

type Reporter interface {
	ReportPanic(context.Context, Incident)
}

type ReporterFunc func(context.Context, Incident)

func (reporter ReporterFunc) ReportPanic(ctx context.Context, incident Incident) {
	reporter(ctx, incident)
}

type HealthMarker interface {
	MarkDegraded(context.Context, string, string)
}

type HealthMarkerFunc func(context.Context, string, string)

func (marker HealthMarkerFunc) MarkDegraded(ctx context.Context, boundary string, reason string) {
	marker(ctx, boundary, reason)
}

type Options struct {
	Boundary  string
	RequestID string
	TraceID   string
	Caller    string
	Reporter  Reporter
	Health    HealthMarker
}

func Run(ctx context.Context, options Options, operation func(context.Context) error) (err error) {
	if ctx == nil || operation == nil || strings.TrimSpace(options.Boundary) == "" {
		failure, _ := fault.New(fault.Spec{
			Code: fault.CodeInvalidArgument, ReasonCode: "core.invalid_argument",
			MessageKey: "errors.invalid_argument", UserMessage: "操作参数无效",
		})
		return failure
	}
	defer func() {
		if recover() == nil {
			return
		}
		stack := debug.Stack()
		digest := sha256.Sum256(append([]byte(options.Boundary+"\n"), stack...))
		incident := Incident{Boundary: options.Boundary, Fingerprint: hex.EncodeToString(digest[:]), Stack: append([]byte(nil), stack...)}
		safely(func() {
			if options.Reporter != nil {
				options.Reporter.ReportPanic(ctx, incident)
			}
		})
		safely(func() {
			if options.Health != nil {
				options.Health.MarkDegraded(ctx, options.Boundary, "core.internal_panic")
			}
		})
		details := map[string]string{
			"fingerprint": incident.Fingerprint,
			"boundary":    options.Boundary,
		}
		if caller := strings.TrimSpace(options.Caller); caller != "" {
			details["caller"] = caller
		}
		if requestID := strings.TrimSpace(options.RequestID); requestID != "" {
			details["request_id"] = requestID
		}
		if traceID := strings.TrimSpace(options.TraceID); traceID != "" {
			details["trace_id"] = traceID
		}
		failure, createErr := fault.New(fault.Spec{
			Code: fault.CodeInternal, ReasonCode: "core.internal_panic", MessageKey: "errors.internal_panic",
			UserMessage: "操作发生内部错误", RequestID: options.RequestID, TraceID: options.TraceID,
			SafeDetails: details,
		})
		if createErr != nil {
			err = fault.Normalize(createErr)
			return
		}
		err = failure
	}()
	return operation(ctx)
}

func safely(operation func()) {
	defer func() { _ = recover() }()
	operation()
}
