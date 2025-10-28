# Go Loadgen

This is a small library that provides utilities for load testing. It is protocol-agnostic, you can use it to test any HTTP, gRPC, or other services.
It is based on a simple API : to test a service, you need to define a client, a data provider, and a collector.
Go Loadgen then takes care of executing the workload and collecting the results.
It also provides a simple workload-pattern generator that can generate a workload based on a configuration if you want to create a workload with many different phases which have different RPS.

## Use it in your project
```bash
go get github.com/luccadibe/go-loadgen
```

## Motivation

I used a lot of k6 in the past for load testing, but when I tried to run longer workloads, the resource usage was too high, and it felt like a waste, especially because I didn't need all of the features that k6 provides.

[ghz](https://github.com/bojand/ghz) didn't fit my needs because it stores all results in memory and writes them to disk in the end. This library provides you full flexibility to implement your own collector.

## Features

- Protocol-agnostic
- Type-safe using go generics
- Support for constant and variable RPS
- Support for workload-pattern generation with weighted time allocation
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

// Then, we define our client and data provider. 
// It must implement the Client interface.
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

// Our data provider will provide the data to be sent to the server in each request. 
// It must be thread safe.
type myDataProvider struct{}

func (d *myDataProvider) GetData() endpointRequest {
    // We'll increment the counter by 1 each time
	return endpointRequest{Delta: 1}
}

func main() {
// Then, we define our collector. For this we can use the CSVCollector. 
// We can also provide a flush interval, which will be used to flush the collector 
// to the disk every flushInterval.
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
		// The workload will run until the max duration is reached or the workload is stopped. 
		// All of the results will be collected and written to the CSV file.
		ew.Run()
}
```

A simple library like this gives me flexibility to test any service and avoid re writing the same executor code each time.

## Workload Pattern Generation

If you want to create more complex workloads with randomized phases, you can use the workload pattern generation feature. 
This is useful when you want to simulate variable traffic patterns without having to define each phase manually.

```go
// Using the same client and data provider from the previous example
func main() {
	collector, err := go_loadgen.NewCSVCollector[endpointResponse]("results.csv", 1*time.Second)
	if err != nil {
		log.Fatalf("Failed to create collector: %v", err)
	}
	// Don't forget to close the collector when you're done
	defer collector.Close()

	// Create a workload with pattern generation enabled
	ew, err := go_loadgen.NewEndpointWorkload(
		"increment",
		&go_loadgen.Config{
			// Enable workload generation
			GenerateWorkload: true,
			MaxDuration:      60 * time.Second,
			Timeout:          10,
			// Define patterns instead of specific phases
			Patterns: []*go_loadgen.PhasePattern{
				{
					Name:               "increment",
					// Generate between 3 and 8 phases
					PhaseCount:         go_loadgen.IntRange{Min: 3, Max: 8},
					// 60% chance of constant RPS phases
					ConstantLikelihood: 0.6,
					// 40% chance of variable RPS phases
					RampingLikelihood:  0.4,
					// This pattern takes up 100% of the workload time (default behavior)
					Weight: 1.0,
					Parameters: go_loadgen.PhaseParameters{
						// Start RPS between 5 and 20
						StartRPS: go_loadgen.IntRange{Min: 5, Max: 20},
						// End RPS between 30 and 100
						EndRPS:   go_loadgen.IntRange{Min: 30, Max: 100},
						// Step size between 1 and 5
						Step:     go_loadgen.IntRange{Min: 1, Max: 5},
					},
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

	// The generator will create a randomized workload based on your patterns
	// Each run will produce different phases within your specified parameters
	ew.Run()
}
```

## Pattern Weighting

You can control how much time each pattern takes up in your workload using the `Weight` field. 
Weights must sum to 1.0, or if not specified (or all set to 0.0), patterns will be weighted equally.

```go
Patterns: []*go_loadgen.PhasePattern{
	{
		Name:               "heavy_load",
		Weight:              0.7, // 70% of total workload time
		PhaseCount:          go_loadgen.IntRange{Min: 2, Max: 4},
		ConstantLikelihood:  0.8,
		RampingLikelihood:   0.2,
		Parameters: go_loadgen.PhaseParameters{
			StartRPS: go_loadgen.IntRange{Min: 10, Max: 50},
			EndRPS:   go_loadgen.IntRange{Min: 50, Max: 100},
			Step:     go_loadgen.IntRange{Min: 5, Max: 10},
		},
	},
	{
		Name:               "light_load",
		Weight:              0.3, // 30% of total workload time
		PhaseCount:          go_loadgen.IntRange{Min: 1, Max: 2},
		ConstantLikelihood:  1.0,
		RampingLikelihood:   0.0,
		Parameters: go_loadgen.PhaseParameters{
			StartRPS: go_loadgen.IntRange{Min: 1, Max: 5},
			EndRPS:   go_loadgen.IntRange{Min: 5, Max: 10},
			Step:     go_loadgen.IntRange{Min: 1, Max: 2},
		},
	},
},
```

This will create a workload where the "heavy_load" pattern takes up 70% of the total time, 
and "light_load" takes up 30% of the time.

**Note**: Patterns are executed in the order they appear in the slice. The "heavy_load" pattern will always execute before "light_load" in this example.

If you want to run the examples, you can use the justfile:

```bash
just run-example http server
# in another terminal
just run-example http client
```

## Contributing
This is my first public library, so any feedback or contribution is welcome.
Please feel free to open an issue or submit a pull request.

## License

Apache License 2.0 - see the [LICENSE](LICENSE) file for details.