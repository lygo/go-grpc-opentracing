package grpc_opentracing

import (
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/opentracing/opentracing-go/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"
)

func OpenTracingServerUnaryInterceptor() grpc.UnaryServerInterceptor {
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
		spanContext, err := opentracing.GlobalTracer().Extract(opentracing.HTTPHeaders, metadataReaderWriter{md})
		if err != nil && err != opentracing.ErrSpanContextNotFound {
			// TODO: establish some sort of error reporting mechanism here. We
			// don't know where to put such an error and must rely on Tracer
			// implementations to do something appropriate for the time being.
		}
		serverSpan := opentracing.GlobalTracer().StartSpan(
			info.FullMethod,
			ext.RPCServerOption(spanContext),
			gRPCComponentTag,
		)
		defer serverSpan.Finish()

		ctx = opentracing.ContextWithSpan(ctx, serverSpan)
		serverSpan.LogFields(log.Object("gRPC request", req))
		resp, err = handler(ctx, req)
		if err == nil {
			serverSpan.LogFields(log.Object("gRPC response", resp))
		} else {
			ext.Error.Set(serverSpan, true)
			serverSpan.LogFields(log.String("event", "gRPC error"), log.Error(err))
		}
		return resp, err
	}
}

func OpenTracingServerStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		md, ok := metadata.FromContext(ctx)
		if !ok {
			md = metadata.New(nil)
		}

		spanContext, err := opentracing.GlobalTracer().Extract(opentracing.HTTPHeaders, metadataReaderWriter{md})
		if err != nil && err != opentracing.ErrSpanContextNotFound {
			grpclog.Printf(err.Error())
		}
		serverSpan := opentracing.GlobalTracer().StartSpan(
			info.FullMethod,
			ext.RPCServerOption(spanContext),
			gRPCComponentTag,
		)
		ext.SpanKindRPCServer.Set(serverSpan)
		defer serverSpan.Finish()
		ctx = opentracing.ContextWithSpan(ctx, serverSpan)
		serverSpan.LogFields(log.Object("gRPC", "open steam"))

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
	ext.Component.Set(span, "gRPC")
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
	ext.Component.Set(span, "gRPC")
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
