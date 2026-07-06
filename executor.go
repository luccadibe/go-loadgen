package go_loadgen

import (
	"context"
	"sync"
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

	scheduler := newRateScheduler(e.rps)
	t := time.NewTicker(MIN_INTERVAL)
	defer t.Stop()
	for {
		select {
		case <-subCtx.Done():
			return
		case <-e.stopChan:
			return
		case <-t.C:
			for range scheduler.requestsThisTick() {
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

	// sub ticker, used to dispatch requests at MIN_INTERVAL
	scheduler := newRateScheduler(*currentRPS)
	subt := time.NewTicker(MIN_INTERVAL)
	defer subt.Stop()

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

				scheduler.update(*currentRPS)
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
			for range scheduler.requestsThisTick() {
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

func calculateInterval(rps int) time.Duration {
	return max(time.Second/time.Duration(rps), MIN_INTERVAL)
}

// rateScheduler dispatches requests at the target RPS using fractional-rate
// accumulation when MIN_INTERVAL caps the tick rate below the target RPS.
type rateScheduler struct {
	mu          sync.Mutex
	rps         int
	tickEvery   time.Duration
	ticksPerSec int
	accumulator int
}

func newRateScheduler(rps int) *rateScheduler {
	s := &rateScheduler{}
	s.update(rps)
	return s
}

func (s *rateScheduler) update(rps int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.rps = rps
	s.tickEvery = MIN_INTERVAL
	s.ticksPerSec = int(time.Second / MIN_INTERVAL)
	s.accumulator = 0
}

func (s *rateScheduler) tickInterval() time.Duration {
	return MIN_INTERVAL
}

func (s *rateScheduler) requestsThisTick() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.rps <= 0 || s.ticksPerSec <= 0 {
		return 0
	}

	s.accumulator += s.rps
	n := s.accumulator / s.ticksPerSec
	s.accumulator %= s.ticksPerSec
	return n
}

// offeredRPSOverTicks simulates dispatch over n ticks and returns total requests.
// Used by tests to verify average offered rate without timing-dependent integration tests.
func offeredRPSOverTicks(rps int, ticks int) int {
	scheduler := newRateScheduler(rps)
	total := 0
	for range ticks {
		total += scheduler.requestsThisTick()
	}
	return total
}
