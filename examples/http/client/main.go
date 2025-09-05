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

func main() {

	startTime := time.Now()

	collector, err := go_loadgen.NewCSVCollector[endpointResponse]("results.csv", 1*time.Second)
	if err != nil {
		fmt.Println("Error creating collector:", err)
		return
	}
	defer collector.Close()

	genericRunner, err := go_loadgen.NewEndpointWorkload(
		"increment",
		&go_loadgen.Config{
			GenerateWorkload: false,
			MaxDuration:      20 * time.Second,
			Timeout:          10,
			Phases: []go_loadgen.TestPhase{
				{
					Name:      "increment",
					Type:      "constant",
					StartTime: 0,
					Duration:  10 * time.Second,
					StartRPS:  1,
					Step:      1,
				},
				{
					Name:      "increment",
					Type:      "variable",
					StartTime: 10 * time.Second,
					Duration:  10 * time.Second,
					StartRPS:  10,
					EndRPS:    100,
					Step:      10,
				},
			},
		},
		&simpleClient{},
		&simpleDataProvider{},
		collector,
	)

	if err != nil {
		fmt.Println("Error creating genericRunner:", err)
		return
	}

	genericRunner.Run()

	fmt.Println("Finished running genericRunner in", time.Since(startTime))

	// with workload generation

	startTime = time.Now()

	genericRunner2, err := go_loadgen.NewEndpointWorkload(
		"increment",
		&go_loadgen.Config{
			GenerateWorkload: true,
			MaxDuration:      20 * time.Second,
			Timeout:          10,
			Patterns: map[string]*go_loadgen.PhasePattern{
				"/increment": {
					Endpoint:           "/increment",
					PhaseCount:         go_loadgen.IntRange{Min: 1, Max: 10},
					ConstantLikelihood: 0.5,
					RampingLikelihood:  0.5,
					Parameters: go_loadgen.PhaseParameters{
						StartRPS: go_loadgen.IntRange{Min: 1, Max: 10},
						EndRPS:   go_loadgen.IntRange{Min: 20, Max: 30},
						Step:     go_loadgen.IntRange{Min: 1, Max: 10},
					},
				},
			},
		},
		&simpleClient{},
		&simpleDataProvider{},
		collector,
	)

	if err != nil {
		fmt.Println("Error creating genericRunner2:", err)
		return
	}

	genericRunner2.Run()

	fmt.Println("Finished running genericRunner2 in", time.Since(startTime))
}
