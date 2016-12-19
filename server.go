package otgrpc

import (
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/opentracing/opentracing-go/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"
)

// OpenTracingServerInterceptor returns a grpc.UnaryServerInterceptor suitable
// for use in a grpc.NewServer call.
//
// For example:
//
//     s := grpc.NewServer(
//         ...,  // (existing ServerOptions)
//         grpc.UnaryInterceptor(otgrpc.OpenTracingServerInterceptor(tracer)))
//
// All gRPC server spans will look for an OpenTracing SpanContext in the gRPC
// metadata; if found, the server span will act as the ChildOf that RPC
// SpanContext.
//
// Root or not, the server Span will be embedded in the context.Context for the
// application-specific gRPC handler(s) to access.
func OpenTracingServerInterceptor(tracer opentracing.Tracer, optFuncs ...Option) grpc.UnaryServerInterceptor {
	otgrpcOpts := newOptions()
	otgrpcOpts.apply(optFuncs...)
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp interface{}, err error) {
		md, ok := metadata.FromContext(ctx)
		if !ok {
			md = metadata.New(nil)
		}
		spanContext, err := tracer.Extract(opentracing.HTTPHeaders, metadataReaderWriter{md})
		if err != nil && err != opentracing.ErrSpanContextNotFound {
			// TODO: establish some sort of error reporting mechanism here. We
			// don't know where to put such an error and must rely on Tracer
			// implementations to do something appropriate for the time being.
		}
		serverSpan := tracer.StartSpan(
			info.FullMethod,
			ext.RPCServerOption(spanContext),
			gRPCComponentTag,
		)
		defer serverSpan.Finish()

		ctx = opentracing.ContextWithSpan(ctx, serverSpan)
		if otgrpcOpts.logPayloads {
			serverSpan.LogFields(log.Object("gRPC request", req))
		}
		resp, err = handler(ctx, req)
		if err == nil {
			if otgrpcOpts.logPayloads {
				serverSpan.LogFields(log.Object("gRPC response", resp))
			}
		} else {
			ext.Error.Set(serverSpan, true)
			serverSpan.LogFields(log.String("event", "gRPC error"), log.Error(err))
		}
		if otgrpcOpts.decorator != nil {
			otgrpcOpts.decorator(serverSpan, info.FullMethod, req, resp, err)
		}
		return resp, err
	}
}

func OpenTracingServerStreamInterceptor(tracer opentracing.Tracer, optFuncs ...Option) grpc.StreamServerInterceptor {
	otgrpcOpts := newOptions()
	otgrpcOpts.apply(optFuncs...)

	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		md, ok := metadata.FromContext(ctx)
		if !ok {

			md = metadata.New(nil)
		}

		spanContext, err := tracer.Extract(opentracing.HTTPHeaders, metadataReaderWriter{md})
		if err != nil && err != opentracing.ErrSpanContextNotFound {
			grpclog.Printf(err.Error())
		}
		serverSpan := tracer.StartSpan(
			info.FullMethod,
			ext.RPCServerOption(spanContext),
			gRPCComponentTag,
		)
		ext.SpanKindRPCServer.Set(serverSpan)
		defer serverSpan.Finish()
		ctx = opentracing.ContextWithSpan(ctx, serverSpan)
		if otgrpcOpts.logPayloads {
			serverSpan.LogFields(log.Object("gRPC", "open steam"))
		}

		err = handler(srv, &serverStreamSpy{ss, serverSpan, ctx})
		if err != nil {
			ext.Error.Set(serverSpan, true)
			serverSpan.LogFields(log.String("event", "gRPC error"), log.Error(err))
		}
		return err
	}
}

type serverStreamSpy struct {
	grpc.ServerStream
	parentSpan opentracing.Span
	parentCtx  context.Context
}

func (s *serverStreamSpy) SendMsg(m interface{}) error {
	span, _ := opentracing.StartSpanFromContext(s.parentCtx, "Send msg")
	ext.SpanKindRPCServer.Set(span)
	ext.Component.Set(span, "grpc")
	var v string
	if m == nil {
		v = `nil`
	} else if str, ok := (m).(Stringer); ok {
		v = str.String()
	}
	span.SetTag(OpenTracingTagStreamSend, v)

	defer span.Finish()

	err := s.ServerStream.SendMsg(m)
	if err != nil {
		ext.Error.Set(span, true)
		span.SetTag(OpenTracingTagGrpcCode, grpc.Code(err))
		span.SetTag(OpenTracingTagGrpcError, err.Error())
	}
	return err
}

func (s *serverStreamSpy) RecvMsg(m interface{}) error {
	span, _ := opentracing.StartSpanFromContext(s.parentCtx, "Recv msg")
	ext.SpanKindRPCServer.Set(span)
	ext.Component.Set(span, "grpc")
	defer span.Finish()

	err := s.ServerStream.RecvMsg(m)
	if err != nil {
		ext.Error.Set(span, true)
		span.SetTag(OpenTracingTagGrpcCode, grpc.Code(err))
		span.SetTag(OpenTracingTagGrpcError, err.Error())
	} else {
		var v string
		if m == nil {
			v = `nil`
		} else if str, ok := (m).(Stringer); ok {
			v = str.String()
		}

		span.SetTag(OpenTracingTagStreamRecv, v)
	}
	return err
}
