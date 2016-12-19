package otgrpc

import (
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/opentracing/opentracing-go/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"io"
)

const OpenTracingTagStream = "grpc.stream"
const OpenTracingTagStreamSend = "grpc.stream.send"
const OpenTracingTagStreamRecv = "grpc.stream.recv"
const OpenTracingTagGrpcError = "grpc.stream.error"
const OpenTracingTagGrpcCode = "grpc.code"

// OpenTracingClientInterceptor returns a grpc.UnaryClientInterceptor suitable
// for use in a grpc.Dial call.
//
// For example:
//
//     conn, err := grpc.Dial(
//         address,
//         ...,  // (existing DialOptions)
//         grpc.WithUnaryInterceptor(otgrpc.OpenTracingClientInterceptor(tracer)))
//
// All gRPC client spans will inject the OpenTracing SpanContext into the gRPC
// metadata; they will also look in the context.Context for an active
// in-process parent Span and establish a ChildOf reference if such a parent
// Span could be found.
func OpenTracingClientInterceptor(tracer opentracing.Tracer, optFuncs ...Option) grpc.UnaryClientInterceptor {
	otgrpcOpts := newOptions()
	otgrpcOpts.apply(optFuncs...)
	return func(
		ctx context.Context,
		method string,
		req, resp interface{},
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		var err error
		var parentCtx opentracing.SpanContext
		if parent := opentracing.SpanFromContext(ctx); parent != nil {
			parentCtx = parent.Context()
		}
		clientSpan := tracer.StartSpan(
			method,
			opentracing.ChildOf(parentCtx),
			ext.SpanKindRPCClient,
			gRPCComponentTag,
		)
		defer clientSpan.Finish()
		md, ok := metadata.FromContext(ctx)
		if !ok {
			md = metadata.New(nil)
		}
		mdWriter := metadataReaderWriter{md}

		err = tracer.Inject(clientSpan.Context(), opentracing.HTTPHeaders, mdWriter)
		// We have no better place to record an error than the Span itself :-/
		if err != nil {
			clientSpan.LogFields(log.String("event", "Tracer.Inject() failed"), log.Error(err))
		}
		ctx = metadata.NewContext(ctx, md)
		if otgrpcOpts.logPayloads {
			clientSpan.LogFields(log.Object("gRPC request", req))
		}
		err = invoker(ctx, method, req, resp, cc, opts...)
		if err == nil {
			if otgrpcOpts.logPayloads {
				clientSpan.LogFields(log.Object("gRPC response", resp))
			}
		} else {
			clientSpan.LogFields(log.String("event", "gRPC error"), log.Error(err))
			ext.Error.Set(clientSpan, true)
		}
		if otgrpcOpts.decorator != nil {
			otgrpcOpts.decorator(clientSpan, method, req, resp, err)
		}
		return err
	}
}

func OpenTracingClientStreamInterceptor(tracer opentracing.Tracer, optFuncs ...Option) grpc.StreamClientInterceptor {
	otgrpcOpts := newOptions()
	otgrpcOpts.apply(optFuncs...)
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {

		var err error

		clientSpan, ctx := opentracing.StartSpanFromContext(ctx, "GRPS stream "+method)
		clientSpan.SetTag(OpenTracingTagStream, desc.StreamName)
		ext.SpanKindRPCClient.Set(clientSpan)
		ext.Component.Set(clientSpan, "grpc")
		md, ok := metadata.FromContext(ctx)
		if !ok {
			md = metadata.New(nil)
		}
		mdWriter := metadataReaderWriter{md}
		err = tracer.Inject(clientSpan.Context(), opentracing.HTTPHeaders, mdWriter)
		// We have no better place to record an error than the Span itself :-/
		if err != nil {
			clientSpan.LogFields(log.String("event", "Tracer.Inject() failed"), log.Error(err))
		}
		ctx = metadata.NewContext(ctx, md)
		if otgrpcOpts.logPayloads {
			clientSpan.LogFields(log.Object("gRPC stream", desc.StreamName))
		}
		clientStream, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			clientSpan.SetTag(OpenTracingTagGrpcError, err.Error())
			clientSpan.LogFields(log.String("event", "gRPC open stream error"), log.Error(err))
			ext.Error.Set(clientSpan, true)
			return nil, err
		}

		return &clientStreamSpy{clientStream, method, desc.StreamName, clientSpan, ctx}, nil
	}
}

type Stringer interface {
	String() string
}

type clientStreamSpy struct {
	grpc.ClientStream
	method     string
	name       string
	parentSpan opentracing.Span
	parentCtx  context.Context
}

func (c *clientStreamSpy) SendMsg(m interface{}) error {
	clientSpan, _ := opentracing.StartSpanFromContext(c.parentCtx, "GRPC send "+c.method)
	ext.SpanKindRPCClient.Set(clientSpan)
	ext.Component.Set(clientSpan, "grpc")
	var v string
	if m == nil {
		v = `nil`
	} else if str, ok := (m).(Stringer); ok {
		v = str.String()
	}
	clientSpan.SetTag(OpenTracingTagStreamSend, v)
	defer clientSpan.Finish()
	err := c.ClientStream.SendMsg(m)
	if err != nil {
		ext.Error.Set(clientSpan, true)
		clientSpan.SetTag(OpenTracingTagGrpcCode, grpc.Code(err))
		clientSpan.SetTag(OpenTracingTagGrpcError, err.Error())
	}
	return err
}

func (c *clientStreamSpy) RecvMsg(m interface{}) error {
	clientSpan, _ := opentracing.StartSpanFromContext(c.parentCtx, "GRPC recv "+c.method)
	ext.SpanKindRPCClient.Set(clientSpan)
	ext.Component.Set(clientSpan, "grpc")
	defer clientSpan.Finish()
	err := c.ClientStream.RecvMsg(m)
	if err == nil {
		var v string
		if m == nil {
			v = `nil`
		} else if str, ok := (m).(Stringer); ok {
			v = str.String()

		}
		clientSpan.SetTag(OpenTracingTagStreamRecv, v)
	} else if err == io.EOF {
		clientSpan.SetTag(OpenTracingTagGrpcCode, codes.OK)
		c.parentSpan.LogFields(log.Object("gRPC close stream", c.name))
		c.parentSpan.Finish()
	} else if err != nil {
		ext.Error.Set(clientSpan, true)
		clientSpan.SetTag(OpenTracingTagGrpcCode, grpc.Code(err))
		clientSpan.SetTag(OpenTracingTagGrpcError, err.Error())
	}
	return err
}
