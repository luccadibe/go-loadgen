# Go Loadgen

This is a small library that provides utilities for load testing. It is protocol-agnostic, you can use it to test any HTTP, gRPC, or other services.
It is based on a simple API : to test a service, you need to define a client, a data provider, and a collector.
Go Loadgen then takes care of executing the workload and collecting the results.
It also provides a simple workload-pattern generator that can generate a workload based on a configuration if you want to create a workload with many different phases which have different RPS.

## Motivation

I used a lot of k6 in the past for load testing, but when I tried to run longer workloads, the resource usage was too high, and it felt like a waste, especially because I didn't need all of the features that k6 provides.

[ghz](https://github.com/bojand/ghz) didn't fit my needs because it stores all results in memory and writes them to disk in the end. This library provides you full flexibility to implement your own collector.

## Features

- Protocol-agnostic
- Type-safe using go generics
- Support for constant and variable RPS
- Support for workload-pattern generation
- No external dependencies

## Example

Say you want to test a gRPC server's "/increment" endpoint with a variable RPS. You can do it like this:

```go

// First, we define our data model: a request and a response.
type endpointRequest struct {
    // We need to send a delta to the endpoint to change the counter
    Delta int32 `json:"delta"`
}

type endpointResponse struct {
    // We'd like to track the latency of the request
	Latency time.Duration `json:"latency"`
    // We'd like to track the counter value
    Counter int32         `json:"counter"`
    // We'd like to track any errors that occur
    Error   string        `json:"error,omitempty"`
}

// Then, we define our client and data provider. Our client will be used to send the request to the server, it must implement the Client interface.
type myClient struct{}
// CallEndpoint will be called by executors using data (endpointRequest) provided by our data provider.
func (c *myClient) CallEndpoint(ctx context.Context, req endpointRequest) endpointResponse {
    // We can track the latency of the request
	startTime := time.Now()
	body, err := json.Marshal(req)
	if err != nil {
		return endpointResponse{Error: err.Error()}
	}
    // We can send an http request to a server.
	resp, err := http.Post("http://localhost:8080/increment", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return endpointResponse{Error: err.Error()}
	}
	defer resp.Body.Close()

	var respBody endpointResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return endpointResponse{Error: err.Error()}
	}
	respBody.Latency = time.Since(startTime)
	return respBody
}

// Our data provider will provide the data to be sent to the server in each request. It must be thread safe.
type myDataProvider struct{}

func (d *myDataProvider) GetData() endpointRequest {
    // We'll increment the counter by 1 each time
	return endpointRequest{Delta: 1}
}

// Then, we define our collector. For this we can use the CSVCollector. We can also provide a flush interval, which will be used to flush the collector to the disk every flushInterval.
collector, err := go_loadgen.NewCSVCollector[endpointResponse]("results.csv", 1*time.Second)
if err != nil {
    log.Fatalf("Failed to create collector: %v", err)
}
defer collector.Close()

// With all of this, we can create a new EndpointWorkload.
ew, err := go_loadgen.NewEndpointWorkload(
		"increment",
		&go_loadgen.Config{
			GenerateWorkload: false,
			MaxDuration:      20 * time.Second,
			Timeout:          10,
			Phases: []go_loadgen.TestPhase{
				{
					Name:      "increment",
                    // constant RPS
					Type:      "constant",
					StartTime: 0,
					Duration:  10 * time.Second,
					StartRPS:  1,
					Step:      1,
				},
				{
					Name:      "increment",
                    // variable RPS. Increments by 10 every second
					Type:      "variable",
					StartTime: 10 * time.Second,
					Duration:  10 * time.Second,
					StartRPS:  10,
					EndRPS:    100,
					Step:      10,
				},
			},
		},
		&myClient{},
		&myDataProvider{},
		collector,
	)

    if err != nil {
        log.Fatalf("Failed to create endpoint workload: %v", err)
    }
    // The workload will run until the max duration is reached or the workload is stopped. All of the results will be collected and written to the CSV file.
    ew.Run()

```

A simple library like this gives me flexibility to test any service and avoid re writing the same executor code each time.