package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	go_loadgen "github.com/luccadibe/go-loadgen"
)

type endpointRequest struct {
	Delta int32 `json:"delta"`
}

type endpointResponse struct {
	Latency time.Duration `json:"latency"`
	Counter int32         `json:"counter"`
	Error   string        `json:"error,omitempty"`
}

func (r endpointResponse) CSVHeaders() []string {
	return []string{"latency", "counter", "error"}
}

func (r endpointResponse) CSVRecord() []string {
	return []string{r.Latency.String(), strconv.Itoa(int(r.Counter)), r.Error}
}

type simpleClient struct{}

type simpleDataProvider struct{}

func (d *simpleDataProvider) GetData() endpointRequest {
	return endpointRequest{Delta: 1}
}

func (c *simpleClient) CallEndpoint(ctx context.Context, req endpointRequest) endpointResponse {
	startTime := time.Now()
	body, err := json.Marshal(req)
	if err != nil {
		return endpointResponse{Error: err.Error()}
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost:8080/increment", bytes.NewBuffer(body))
	if err != nil {
		return endpointResponse{Error: err.Error()}
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpRequest)
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

func main() {

	startTime := time.Now()

	collector, err := go_loadgen.NewCSVCollector[endpointResponse]("results.csv", 1*time.Second)
	if err != nil {
		fmt.Println("Error creating collector:", err)
		return
	}
	defer collector.Close()

	endpoint, err := go_loadgen.NewEndpoint(&simpleClient{}, &simpleDataProvider{}, collector)
	if err != nil {
		fmt.Println("Error creating endpoint:", err)
		return
	}
	workload, err := go_loadgen.NewWorkload(go_loadgen.Spec{
		Duration: 20 * time.Second,
		Endpoints: map[string]go_loadgen.Endpoint{
			"increment": endpoint,
		},
		Phases: []go_loadgen.Phase{
			{
				Duration: 10 * time.Second,
				RPS:      10,
				Targets:  []go_loadgen.Target{{Endpoint: "increment", Weight: 1}},
			},
			{
				StartAt:  10 * time.Second,
				Duration: 10 * time.Second,
				RPS:      10,
				Ramp:     &go_loadgen.Ramp{To: 100, Step: 10, Every: time.Second},
				Targets:  []go_loadgen.Target{{Endpoint: "increment", Weight: 1}},
			},
		},
	})

	if err != nil {
		fmt.Println("Error creating workload:", err)
		return
	}

	report := workload.Run(context.Background())
	fmt.Printf("Finished workload in %s: %+v\n", time.Since(startTime), report)
}
