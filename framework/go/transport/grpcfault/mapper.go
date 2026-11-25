package grpcfault

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/core/fault"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

const errorDomain = "mz-gui-framework"

func ToStatus(err error) error {
	if err == nil {
		return nil
	}
	var frameworkError *fault.Error
	if !errors.As(err, &frameworkError) {
		if _, ok := status.FromError(err); ok {
			return err
		}
	}
	public := fault.Public(err)
	result := status.New(toGRPCCode(public.Code), public.UserMessage)
	metadata := map[string]string{
		"framework_code": string(public.Code),
		"message_key":    public.MessageKey,
		"retryable":      strconv.FormatBool(public.Retryable),
	}
	if public.RequestID != "" {
		metadata["request_id"] = public.RequestID
	}
	if public.TraceID != "" {
		metadata["trace_id"] = public.TraceID
	}
	for key, value := range public.SafeDetails {
		metadata["detail."+key] = value
	}
	if enriched, detailErr := result.WithDetails(&errdetails.ErrorInfo{Reason: public.ReasonCode, Domain: errorDomain, Metadata: metadata}); detailErr == nil {
		result = enriched
	}
	if public.RetryAfterMS > 0 {
		if enriched, detailErr := result.WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(time.Duration(public.RetryAfterMS) * time.Millisecond)}); detailErr == nil {
			result = enriched
		}
	}
	if len(public.FieldIssues) > 0 {
		violations := make([]*errdetails.BadRequest_FieldViolation, 0, len(public.FieldIssues))
		for _, issue := range public.FieldIssues {
			violations = append(violations, &errdetails.BadRequest_FieldViolation{Field: issue.Path, Description: issue.MessageKey})
		}
		if enriched, detailErr := result.WithDetails(&errdetails.BadRequest{FieldViolations: violations}); detailErr == nil {
			result = enriched
		}
	}
	return result.Err()
}

func FromStatus(err error) *fault.Error {
	if err == nil {
		return nil
	}
	grpcStatus := status.Convert(err)
	spec := fault.Spec{
		Code: fromGRPCCode(grpcStatus.Code()), ReasonCode: "transport.grpc_status",
		MessageKey: "errors.grpc_status", UserMessage: defaultMessage(fromGRPCCode(grpcStatus.Code())), Cause: err,
	}
	trusted := false
	for _, detail := range grpcStatus.Details() {
		switch value := detail.(type) {
		case *errdetails.ErrorInfo:
			if value.Domain != errorDomain {
				continue
			}
			trusted = true
			if code, ok := frameworkCode(value.Metadata["framework_code"]); ok {
				spec.Code = code
			}
			spec.ReasonCode = value.Reason
			spec.MessageKey = value.Metadata["message_key"]
			spec.Retryable, _ = strconv.ParseBool(value.Metadata["retryable"])
			spec.RequestID = value.Metadata["request_id"]
			spec.TraceID = value.Metadata["trace_id"]
			spec.SafeDetails = make(map[string]string)
			for key, item := range value.Metadata {
				if strings.HasPrefix(key, "detail.") {
					spec.SafeDetails[strings.TrimPrefix(key, "detail.")] = item
				}
			}
		case *errdetails.RetryInfo:
			if value.RetryDelay != nil {
				spec.RetryAfter = value.RetryDelay.AsDuration()
				spec.Retryable = true
			}
		case *errdetails.BadRequest:
			for _, violation := range value.FieldViolations {
				spec.FieldIssues = append(spec.FieldIssues, fault.FieldIssue{
					Path: violation.Field, Rule: "invalid", MessageKey: violation.Description,
				})
			}
		}
	}
	if trusted {
		spec.UserMessage = grpcStatus.Message()
	}
	failure, createErr := fault.New(spec)
	if createErr != nil {
		return fault.Normalize(err)
	}
	return failure
}

func frameworkCode(value string) (fault.Code, bool) {
	for _, code := range fault.AllCodes() {
		if string(code) == value {
			return code, true
		}
	}
	return "", false
}

func toGRPCCode(code fault.Code) codes.Code {
	switch code {
	case fault.CodeInvalidArgument:
		return codes.InvalidArgument
	case fault.CodeNotFound:
		return codes.NotFound
	case fault.CodeAlreadyExists:
		return codes.AlreadyExists
	case fault.CodeConflict:
		return codes.Aborted
	case fault.CodeUnauthenticated:
		return codes.Unauthenticated
	case fault.CodePermissionDenied, fault.CodeCapabilityDenied:
		return codes.PermissionDenied
	case fault.CodeTimeout:
		return codes.DeadlineExceeded
	case fault.CodeCancelled:
		return codes.Canceled
	case fault.CodeUnavailable:
		return codes.Unavailable
	case fault.CodeResourceExhausted:
		return codes.ResourceExhausted
	case fault.CodeDataLoss:
		return codes.DataLoss
	case fault.CodeIncompatible:
		return codes.FailedPrecondition
	default:
		return codes.Internal
	}
}

func fromGRPCCode(code codes.Code) fault.Code {
	switch code {
	case codes.InvalidArgument:
		return fault.CodeInvalidArgument
	case codes.NotFound:
		return fault.CodeNotFound
	case codes.AlreadyExists:
		return fault.CodeAlreadyExists
	case codes.Aborted:
		return fault.CodeConflict
	case codes.Unauthenticated:
		return fault.CodeUnauthenticated
	case codes.PermissionDenied:
		return fault.CodePermissionDenied
	case codes.DeadlineExceeded:
		return fault.CodeTimeout
	case codes.Canceled:
		return fault.CodeCancelled
	case codes.Unavailable:
		return fault.CodeUnavailable
	case codes.ResourceExhausted:
		return fault.CodeResourceExhausted
	case codes.DataLoss:
		return fault.CodeDataLoss
	case codes.FailedPrecondition:
		return fault.CodeIncompatible
	default:
		return fault.CodeInternal
	}
}

func defaultMessage(code fault.Code) string {
	switch code {
	case fault.CodeCancelled:
		return "操作已取消"
	case fault.CodeTimeout:
		return "操作已超时"
	case fault.CodeUnavailable:
		return "服务暂时不可用"
	case fault.CodeInvalidArgument:
		return "请求参数无效"
	default:
		return "操作发生内部错误"
	}
}
