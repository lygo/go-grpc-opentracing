package grpc_opentracing

import (
	"strings"

	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"google.golang.org/grpc/metadata"
)

var (
	// Morally a const:
	gRPCComponentTag = opentracing.Tag{string(ext.Component), "gRPC"}
)

const (
	OpenTracingTagUnary      = "grpc.unary"
	OpenTracingTagStream     = "grpc.stream"
	OpenTracingTagStreamSend = "grpc.stream.send"
	OpenTracingTagStreamRecv = "grpc.stream.recv"
	OpenTracingTagGrpcError  = "grpc.stream.error"
	OpenTracingTagGrpcCode   = "grpc.code"
)

// metadataReaderWriter satisfies both the opentracing.TextMapReader and
// opentracing.TextMapWriter interfaces.
type metadataReaderWriter struct {
	metadata.MD
}

func (w metadataReaderWriter) Set(key, val string) {
	// The GRPC HPACK implementation rejects any uppercase keys here.
	//
	// As such, since the HTTP_HEADERS format is case-insensitive anyway, we
	// blindly lowercase the key (which is guaranteed to work in the
	// Inject/Extract sense per the OpenTracing spec).
	key = strings.ToLower(key)
	w.MD[key] = append(w.MD[key], val)
}

func (w metadataReaderWriter) ForeachKey(handler func(key, val string) error) error {
	for k, vals := range w.MD {
		for _, v := range vals {
			if dk, dv, err := metadata.DecodeKeyValue(k, v); err == nil {
				if err = handler(dk, dv); err != nil {
					return err
				}
			} else {
				return err
			}
		}
	}

	return nil
}
