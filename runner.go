package go_loadgen

import (
	"context"
	"errors"
	"sync"
	"time"
)

// NewEndpointWorkload creates a new EndpointWorkload. If GenerateWorkload is true, the workload will be generated using the provided patterns. If GenerateWorkload is false, the workload will be executed using the TestPhases in the Config.
func NewEndpointWorkload[C any, R any](name string, config *Config, client Client[C, R], dataProvider DataProvider[C], collector Collector[R]) (*EndpointWorkload[C, R], error) {
	ew := &EndpointWorkload[C, R]{
		Name:         name,
		Config:       config,
		Client:       client,
		DataProvider: dataProvider,
		Collector:    collector,
	}
	if config.GenerateWorkload {
		if config.Patterns == nil {
			return nil, errors.New("workload generation is enabled but no patterns provided")
		}
		generator := NewWorkloadPatternGenerator(config.Seed, config.MaxDuration, config.Timeout, config.Patterns)
		ew.Config.Phases = generator.GenerateWorkload()
	} else if len(config.Phases) == 0 {
		return nil, errors.New("workload generation is disabled but no phases provided")
	}

	return ew, nil
}

// An EndpointWorkload can execute a workload of TestPhases on a provided client. Users must provide their own implementation of Client, DataProvider, and Collector, which all use the same input and output types.
type EndpointWorkload[C any, R any] struct {
	Name         string
	Config       *Config
	Client       Client[C, R]
	DataProvider DataProvider[C]
	Collector    Collector[R]
}

type Config struct {
	GenerateWorkload bool                     `yaml:"generate_workload"`
	Seed             int64                    `yaml:"seed,omitempty"`
	MaxDuration      time.Duration            `yaml:"max_duration"`
	Timeout          int32                    `yaml:"timeout"`
	Patterns         map[string]*PhasePattern `yaml:"patterns"`
	Phases           []TestPhase              `yaml:"phases,omitempty"`
}

type TestPhase struct {
	Name      string        `yaml:"name"`
	Type      string        `yaml:"type"`       // "constant" | "variable"
	StartTime time.Duration `yaml:"start_time"` // Relative to workload start
	Duration  time.Duration `yaml:"duration"`
	StartRPS  int           `yaml:"start_rps"`
	EndRPS    int           `yaml:"end_rps,omitempty"`
	Step      int           `yaml:"step,omitempty"` // For ramping increment/decrement
	Endpoint  string        `yaml:"endpoint"`
}

// A Client is a generic interface that can be used to call an endpoint.
type Client[C any, R any] interface {
	// CallEndpoint should send a request using the provided data to the endpoint and return a result.
	CallEndpoint(ctx context.Context, req C) R
}

// A Collector is a generic interface that can be used to collect results from a client. Users are free to implement their own mechanism for saving results to disk. Executors will  call Collector.Collect() concurrently , and Collector.Close() will be called after all results are collected.
type Collector[R any] interface {
	// Collect should collect the result from a client.
	Collect(result R)
	Close()
}

// A DataProvider is a generic interface that can be used to get data for a request. Users are free to implement their own mechanism for getting data. Executors will call DataProvider.GetData() concurrently.
type DataProvider[C any] interface {
	// GetData should return a data object for a request. It must be thread safe.
	GetData() C
}

// Run executes the workload according to the TestPhases in the Config.
func (e *EndpointWorkload[C, R]) Run() {
	ctx, cancel := context.WithTimeout(context.Background(), e.Config.MaxDuration)
	defer cancel()

	wg := sync.WaitGroup{}

	for _, phase := range e.Config.Phases {
		wg.Add(1)
		go func(phase TestPhase) {
			defer wg.Done()

			// wait for phase start time
			<-time.After(phase.StartTime)
			var executor LoadExecutor
			switch phase.Type {
			case "constant":
				executor = NewConstantExecutor(e.Client, e.Collector, e.DataProvider)
			case "variable":
				executor = NewRampingExecutor(e.Client, e.Collector, e.DataProvider)
			}

			executor.Execute(ctx, phase)

		}(phase)
	}

	wg.Wait()

	e.Collector.Close()
}
