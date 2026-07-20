package go_loadgen

import (
	"context"
	"errors"
	"reflect"
)

// Client invokes one endpoint request.
// Implementations must be safe for concurrent use.
type Client[C any, R any] interface {
	CallEndpoint(context.Context, C) R
}

// DataProvider creates one endpoint request.
// Implementations must be safe for concurrent use and should avoid blocking.
type DataProvider[C any] interface {
	GetData() C
}

// Collector receives one completed endpoint result.
// Implementations must be safe for concurrent use.
type Collector[R any] interface {
	Collect(R)
	Close()
}

// Endpoint is a compiled unit of work. Endpoints are created with NewEndpoint.
type Endpoint interface {
	execute(context.Context)
}

type typedEndpoint[C any, R any] struct {
	client    Client[C, R]
	provider  DataProvider[C]
	collector Collector[R]
}

// NewEndpoint adapts typed request generation, invocation, and result collection
// into an endpoint that can be used in a heterogeneous workload.
func NewEndpoint[C any, R any](client Client[C, R], provider DataProvider[C], collector Collector[R]) (Endpoint, error) {
	if isNil(client) || isNil(provider) || isNil(collector) {
		return nil, errors.New("client, provider, and collector must be non-nil")
	}
	return typedEndpoint[C, R]{client: client, provider: provider, collector: collector}, nil
}

func (e typedEndpoint[C, R]) execute(ctx context.Context) {
	e.collector.Collect(e.client.CallEndpoint(ctx, e.provider.GetData()))
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
