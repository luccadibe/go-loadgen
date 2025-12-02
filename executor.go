package go_loadgen

import (
	"context"
	"time"
)

const MIN_INTERVAL = 10 * time.Millisecond

// A LoadExecutor is a generic interface that can be used to execute a workload of TestPhases.
type LoadExecutor interface {
	Execute(ctx context.Context, phase TestPhase)
	Stop()
}

// A ConstantExecutor is a LoadExecutor that executes a workload of TestPhases with a constant RPS.
type ConstantExecutor[C any, R any] struct {
	rps          int
	client       Client[C, R]
	collector    Collector[R]
	dataProvider DataProvider[C]
	stopChan     chan struct{}
}

// NewConstantExecutor creates a new ConstantExecutor.
func NewConstantExecutor[C any, R any](
	client Client[C, R],
	collector Collector[R],
	dataProvider DataProvider[C],
) *ConstantExecutor[C, R] {
	return &ConstantExecutor[C, R]{
		client:       client,
		collector:    collector,
		dataProvider: dataProvider,
		stopChan:     make(chan struct{}),
	}
}

// A RampingExecutor is a LoadExecutor that executes a workload of TestPhases with variable RPS.
type RampingExecutor[C any, R any] struct {
	startRPS     int
	endRPS       int
	step         int
	client       Client[C, R]
	collector    Collector[R]
	dataProvider DataProvider[C]
	stopChan     chan struct{}
}

// NewRampingExecutor creates a new RampingExecutor.
func NewRampingExecutor[C any, R any](
	client Client[C, R],
	collector Collector[R],
	dataProvider DataProvider[C],
) *RampingExecutor[C, R] {
	return &RampingExecutor[C, R]{
		client:       client,
		collector:    collector,
		dataProvider: dataProvider,
		stopChan:     make(chan struct{}),
	}
}

// Execute executes a workload of TestPhases with a constant RPS.
func (e *ConstantExecutor[C, R]) Execute(ctx context.Context, phase TestPhase) {
	e.rps = phase.StartRPS

	subCtx, cancel := context.WithTimeout(ctx, phase.Duration)
	defer cancel()

	//t := time.NewTicker(time.Second)
	interval, restRPS := calculateInterval(e.rps)
	t := time.NewTicker(interval)
	for {
		select {
		case <-subCtx.Done():
			return
		case <-e.stopChan:
			return
		case <-t.C:
			for range restRPS {

				go func() {
					result := e.client.CallEndpoint(ctx, e.dataProvider.GetData())
					e.collector.Collect(result)
				}()

			}
		}
	}
}

// Stop stops the ConstantExecutor.
func (e *ConstantExecutor[C, R]) Stop() {
	e.stopChan <- struct{}{}
	close(e.stopChan)
}

// Execute executes a workload of TestPhases with a variable RPS.
func (e *RampingExecutor[C, R]) Execute(ctx context.Context, phase TestPhase) {
	e.startRPS = phase.StartRPS
	e.endRPS = phase.EndRPS
	e.step = phase.Step
	incrementing := e.step > 0

	subCtx, cancel := context.WithTimeout(ctx, phase.Duration)
	defer cancel()

	currentRPS := &e.startRPS
	if *currentRPS == 0 {
		*currentRPS = 1
	}
	// main ticker, used to update the currentRPS
	t := time.NewTicker(time.Second)
	first := true

	// sub ticker, used to dispatch requests
	var interval time.Duration
	var restRPS int
	interval, restRPS = calculateInterval(*currentRPS)
	subt := time.NewTicker(interval)

	// update currentRPS

	go func() {
		for {
			select {
			case <-subCtx.Done():
				return
			case <-e.stopChan:
				return
			case <-t.C:
				if !first && (incrementing && *currentRPS < e.endRPS || !incrementing && *currentRPS > e.endRPS) {
					*currentRPS += e.step
				}

				if *currentRPS <= 0 {
					break
				}
				first = false

				// update the sub ticker
				interval, restRPS = calculateInterval(*currentRPS)
				subt = time.NewTicker(interval)

			}
		}

	}()

	// dispatch requests

	for {
		select {
		case <-subCtx.Done():
			return
		case <-e.stopChan:
			return
		case <-subt.C:
			for range restRPS {
				go func() {
					result := e.client.CallEndpoint(ctx, e.dataProvider.GetData())
					e.collector.Collect(result)
				}()
			}
		}
	}

}

// Stop stops the RampingExecutor.
func (e *RampingExecutor[C, R]) Stop() {
	e.stopChan <- struct{}{}
	close(e.stopChan)
}

func calculateInterval(rps int) (time.Duration, int) {
	interval := max(time.Second/time.Duration(rps), MIN_INTERVAL)
	restRPS := rps / int(time.Second/interval)

	//log.Printf("Calculated interval: %v, restRPS: %d", interval, restRPS)

	return interval, restRPS
}
