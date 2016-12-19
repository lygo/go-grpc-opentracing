# OpenTracing support for gRPC in Go

The `grpc_opentracing` package makes it easy to add OpenTracing support to gRPC-based
systems in Go.

## Status

Work in progress

## Installation

```
go get github.com/lygo/go-grpc-opentracing
```

## Documentation

See the basic usage examples below and the [package documentation on
godoc.org](https://godoc.org/github.com/lygo/go-grpc-opentracing).

## Client-side usage example

Wherever you call `grpc.Dial`:

```go
// You must have some sort of OpenTracing Tracer instance on hand.
var tracer opentracing.Tracer = ...
...

// Set up a connection to the server peer.
conn, err := grpc.Dial(
    address,
    ... // other options
    grpc.WithUnaryInterceptor(
        grpc_opentracing.OpenTracingClientUnaryInterceptor(),
    ),
    grpc.WithStreamInterceptor(
            grpc_opentracing.OpenTracingClientStreamInterceptor(),
    ),
)

// All future RPC activity involving `conn` will be automatically traced.
```

## Server-side usage example

Wherever you call `grpc.NewServer`:

```go
// You must have some sort of OpenTracing Tracer instance on hand.
var tracer opentracing.Tracer = ...
...

// Initialize the gRPC server.
s := grpc.NewServer(
    ... // other options
    grpc.UnaryInterceptor(
        grpc_opentracing.OpenTracingServerUnaryInterceptor(),
    ),
    grpc.StreamInterceptor(
        grpc_opentracing.OpenTracingServerStreamInterceptor(),
    ),
),

// All future RPC activity involving `s` will be automatically traced.
```

