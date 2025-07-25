package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	go_loadgen "github.com/luccadibe/go-loadgen"
)

type endpointRequest struct {
	Delta int32 `json:"delta"`
}

type endpointResponse struct {
	Counter int32  `json:"counter"`
	Error   string `json:"error,omitempty"`
}

type simpleClient struct{}

type simpleDataProvider struct{}

func (d *simpleDataProvider) GetData() endpointRequest {
	return endpointRequest{Delta: 1}
}

type simpleCollector struct{}

func (c *simpleCollector) Collect(result endpointResponse) {
	fmt.Println("Collected:	", result.Counter)
}

func (c *simpleCollector) Close() {
	fmt.Println("Closing collector")
}

func (c *simpleClient) CallEndpoint(ctx context.Context, req endpointRequest) endpointResponse {
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
	return respBody
}

func main() {
	client := &simpleClient{}

	startTime := time.Now()

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
		client,
		&simpleDataProvider{},
		&simpleCollector{},
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
		client,
		&simpleDataProvider{},
		&simpleCollector{},
	)

	if err != nil {
		fmt.Println("Error creating genericRunner2:", err)
		return
	}

	genericRunner2.Run()

	fmt.Println("Finished running genericRunner2 in", time.Since(startTime))
}
