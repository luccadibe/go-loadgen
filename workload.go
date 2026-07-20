package go_loadgen

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

const schedulerResolution = time.Millisecond

// Target assigns part of a phase's offered rate to an endpoint. Weight must be positive.
type Target struct {
	Endpoint string
	Weight   uint32
}

// Ramp changes a phase's offered rate by Step every Every interval, ending at To.
// To may be lower than the phase RPS.
type Ramp struct {
	To    uint64
	Step  uint64
	Every time.Duration
}

// Phase schedules an open-loop offered rate. RPS is the total rate before target splitting.
type Phase struct {
	StartAt  time.Duration
	Duration time.Duration
	RPS      uint64
	Ramp     *Ramp
	Targets  []Target
}

// Spec describes a workload before endpoint names and target weights are compiled.
type Spec struct {
	Duration  time.Duration
	Seed      uint64
	Endpoints map[string]Endpoint
	Phases    []Phase

	// MaxInFlight bounds outstanding requests. Zero leaves it unbounded.
	// When full, arrivals are dropped so the schedule remains open-loop.
	MaxInFlight uint64
	// DrainTimeout cancels outstanding requests after scheduling ends. Zero waits indefinitely.
	DrainTimeout time.Duration
}

// Report contains the actual load generator outcome. Scheduled is the number of
// arrivals requested by phases; Issued is the number passed to endpoint execution.
type Report struct {
	Scheduled     uint64
	Issued        uint64
	Dropped       uint64
	Missed        uint64
	Completed     uint64
	PeakInFlight  uint64
	DrainTimedOut bool
	// SchedulingDuration ends when no phase can issue another arrival.
	SchedulingDuration time.Duration
	// Duration includes the post-scheduling drain.
	Duration time.Duration
}

// Workload is an immutable, validated workload ready to run.
type Workload struct {
	duration     time.Duration
	seed         uint64
	phases       []compiledPhase
	maxInFlight  uint64
	drainTimeout time.Duration
}

type compiledPhase struct {
	phase   Phase
	chooser aliasChooser
	seed    uint64
}

// NewWorkload validates a workload and compiles endpoint routing. It performs no
// allocation or endpoint lookup during request dispatch.
func NewWorkload(spec Spec) (*Workload, error) {
	if spec.Duration <= 0 {
		return nil, errors.New("workload duration must be positive")
	}
	if len(spec.Phases) == 0 {
		return nil, errors.New("workload must contain at least one phase")
	}
	if len(spec.Endpoints) == 0 {
		return nil, errors.New("workload must contain at least one endpoint")
	}
	if spec.DrainTimeout < 0 {
		return nil, errors.New("drain timeout cannot be negative")
	}

	w := &Workload{
		duration:     spec.Duration,
		seed:         spec.Seed,
		phases:       make([]compiledPhase, len(spec.Phases)),
		maxInFlight:  spec.MaxInFlight,
		drainTimeout: spec.DrainTimeout,
	}
	for i, phase := range spec.Phases {
		if err := validatePhase(spec.Duration, phase); err != nil {
			return nil, fmt.Errorf("phase %d: %w", i, err)
		}
		endpoints := make([]Endpoint, len(phase.Targets))
		weights := make([]uint32, len(phase.Targets))
		for j, target := range phase.Targets {
			endpoint, ok := spec.Endpoints[target.Endpoint]
			if !ok || isNil(endpoint) {
				return nil, fmt.Errorf("phase %d target %q is not registered", i, target.Endpoint)
			}
			endpoints[j], weights[j] = endpoint, target.Weight
		}
		chooser, err := newAliasChooser(endpoints, weights)
		if err != nil {
			return nil, fmt.Errorf("phase %d: %w", i, err)
		}
		compiled := phase
		if phase.Ramp != nil {
			ramp := *phase.Ramp
			compiled.Ramp = &ramp
		}
		w.phases[i] = compiledPhase{phase: compiled, chooser: chooser, seed: splitMix64(spec.Seed + uint64(i))}
	}
	return w, nil
}

func validatePhase(workloadDuration time.Duration, phase Phase) error {
	if phase.StartAt < 0 || phase.Duration <= 0 {
		return errors.New("start time must be non-negative and duration must be positive")
	}
	if phase.StartAt >= workloadDuration || phase.Duration > workloadDuration-phase.StartAt {
		return errors.New("phase must fit within workload duration")
	}
	if phase.RPS == 0 {
		return errors.New("RPS must be positive")
	}
	if len(phase.Targets) == 0 {
		return errors.New("phase must target at least one endpoint")
	}
	if phase.Ramp != nil {
		if phase.Ramp.Step == 0 || phase.Ramp.Every <= 0 {
			return errors.New("ramp step and interval must be positive")
		}
	}
	return nil
}

// Run issues all phase arrivals, then waits for their completion. The supplied
// context is only external cancellation; phase deadlines never cancel requests.
func (w *Workload) Run(ctx context.Context) Report {
	started := time.Now()
	requestsCtx, cancelRequests := context.WithCancel(ctx)
	defer cancelRequests()

	var report runReport
	var schedulers sync.WaitGroup
	var requests sync.WaitGroup
	for i := range w.phases {
		phase := &w.phases[i]
		schedulers.Add(1)
		go func() {
			defer schedulers.Done()
			w.runPhase(ctx, requestsCtx, started, phase, &report, &requests)
		}()
	}
	schedulers.Wait()
	schedulingDuration := time.Since(started)

	var timedOut atomic.Bool
	var timer *time.Timer
	if w.drainTimeout > 0 {
		timer = time.AfterFunc(w.drainTimeout, func() {
			if report.inFlight.Load() != 0 {
				timedOut.Store(true)
				cancelRequests()
			}
		})
	}
	requests.Wait()
	if timer != nil {
		timer.Stop()
	}

	return Report{
		Scheduled:          report.scheduled.Load(),
		Issued:             report.issued.Load(),
		Dropped:            report.dropped.Load(),
		Missed:             report.missed.Load(),
		Completed:          report.completed.Load(),
		PeakInFlight:       report.peakInFlight.Load(),
		DrainTimedOut:      timedOut.Load(),
		SchedulingDuration: schedulingDuration,
		Duration:           time.Since(started),
	}
}

type runReport struct {
	scheduled    atomic.Uint64
	issued       atomic.Uint64
	dropped      atomic.Uint64
	missed       atomic.Uint64
	completed    atomic.Uint64
	inFlight     atomic.Uint64
	peakInFlight atomic.Uint64
}

func (w *Workload) runPhase(controlCtx, requestsCtx context.Context, workloadStart time.Time, phase *compiledPhase, report *runReport, requests *sync.WaitGroup) {
	start := workloadStart.Add(phase.phase.StartAt)
	end := start.Add(phase.phase.Duration)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	if !waitUntilTimer(controlCtx, timer, start) {
		return
	}

	random := phaseRandom{state: phase.seed}
	next := start
	var remainder uint64
	for {
		rate := phase.rateAt(next.Sub(start))
		interval := batchInterval(rate)
		next = next.Add(interval)
		if next.After(end) {
			return
		}
		if !waitUntilTimer(controlCtx, timer, next) {
			return
		}

		// Do not replay arrivals after a loader pause: report them instead of
		// creating an artificial catch-up burst against the target.
		for time.Since(next) >= interval {
			count := arrivalsForInterval(rate, interval, &remainder)
			report.scheduled.Add(count)
			report.missed.Add(count)
			next = next.Add(interval)
			if next.After(end) {
				return
			}
			rate = phase.rateAt(next.Sub(start))
			interval = batchInterval(rate)
		}

		count := arrivalsForInterval(rate, interval, &remainder)
		for range count {
			if controlCtx.Err() != nil {
				return
			}
			report.scheduled.Add(1)
			if !acquire(&report.inFlight, w.maxInFlight, &report.peakInFlight) {
				report.dropped.Add(1)
				continue
			}
			endpoint := phase.chooser.choose(&random)
			report.issued.Add(1)
			requests.Add(1)
			go func() {
				defer requests.Done()
				defer report.inFlight.Add(^uint64(0))
				defer report.completed.Add(1)
				endpoint.execute(requestsCtx)
			}()
		}
	}
}

func (p *compiledPhase) rateAt(elapsed time.Duration) uint64 {
	if p.phase.Ramp == nil {
		return p.phase.RPS
	}
	steps := uint64(elapsed / p.phase.Ramp.Every)
	start, end, step := p.phase.RPS, p.phase.Ramp.To, p.phase.Ramp.Step
	if end > start {
		difference := end - start
		if steps >= (difference-1)/step+1 {
			return end
		}
		return start + steps*step
	}
	difference := start - end
	if steps >= (difference-1)/step+1 {
		return end
	}
	return start - steps*step
}

func batchInterval(rps uint64) time.Duration {
	if rps < 1000 {
		return time.Second / time.Duration(rps)
	}
	return schedulerResolution
}

func arrivalsForInterval(rps uint64, interval time.Duration, remainder *uint64) uint64 {
	if rps < 1000 {
		return 1
	}
	whole, fraction := rps/1000, rps%1000
	*remainder += fraction
	if *remainder >= 1000 {
		whole++
		*remainder -= 1000
	}
	return whole
}

func waitUntilTimer(ctx context.Context, timer *time.Timer, target time.Time) bool {
	delay := time.Until(target)
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer.Reset(delay)
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func acquire(inFlight *atomic.Uint64, maximum uint64, peak *atomic.Uint64) bool {
	for {
		current := inFlight.Load()
		if maximum != 0 && current >= maximum {
			return false
		}
		if inFlight.CompareAndSwap(current, current+1) {
			for current+1 > peak.Load() && !peak.CompareAndSwap(peak.Load(), current+1) {
			}
			return true
		}
	}
}

// aliasChooser implements O(1) weighted endpoint selection. It is immutable
// after workload compilation and each phase owns its random state.
type aliasChooser struct {
	endpoints []Endpoint
	prob      []uint32
	alias     []uint32
}

func newAliasChooser(endpoints []Endpoint, weights []uint32) (aliasChooser, error) {
	var total uint64
	for _, weight := range weights {
		if weight == 0 {
			return aliasChooser{}, errors.New("target weight must be positive")
		}
		total += uint64(weight)
	}
	if total == 0 {
		return aliasChooser{}, errors.New("target weights must not overflow")
	}
	n := len(weights)
	scaled := make([]float64, n)
	small := make([]int, 0, n)
	large := make([]int, 0, n)
	for i, weight := range weights {
		scaled[i] = float64(weight) * float64(n) / float64(total)
		if scaled[i] < 1 {
			small = append(small, i)
		} else {
			large = append(large, i)
		}
	}
	chooser := aliasChooser{endpoints: endpoints, prob: make([]uint32, n), alias: make([]uint32, n)}
	for len(small) > 0 && len(large) > 0 {
		s := small[len(small)-1]
		small = small[:len(small)-1]
		l := large[len(large)-1]
		chooser.prob[s] = uint32(scaled[s] * float64(math.MaxUint32))
		chooser.alias[s] = uint32(l)
		scaled[l] += scaled[s] - 1
		if scaled[l] < 1 {
			large = large[:len(large)-1]
			small = append(small, l)
		}
	}
	for _, index := range append(small, large...) {
		chooser.prob[index] = math.MaxUint32
		chooser.alias[index] = uint32(index)
	}
	return chooser, nil
}

func (c aliasChooser) choose(random *phaseRandom) Endpoint {
	value := random.next()
	index := uint64(uint32(value)) * uint64(len(c.endpoints)) >> 32
	if uint32(value>>32) <= c.prob[index] {
		return c.endpoints[index]
	}
	return c.endpoints[c.alias[index]]
}

type phaseRandom struct{ state uint64 }

func (r *phaseRandom) next() uint64 {
	r.state ^= r.state << 7
	r.state ^= r.state >> 9
	return r.state
}

func splitMix64(value uint64) uint64 {
	value += 0x9e3779b97f4a7c15
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	return value ^ (value >> 31)
}
