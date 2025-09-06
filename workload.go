package go_loadgen

import (
	"math/rand/v2"
	"strconv"
	"time"
)

type WorkloadPatternGenerator struct {
	seed        int64
	random      *rand.Rand
	maxDuration time.Duration
	patterns    map[string]*PhasePattern
}

// PhasePattern is a template for a workload phase. It is used to generate a workload of TestPhases.
type PhasePattern struct {
	Endpoint           string          `yaml:"endpoint"`
	PhaseCount         IntRange        `yaml:"phase_count"`
	ConstantLikelihood float64         `yaml:"constant_likelihood"` // 0.0-1.0
	RampingLikelihood  float64         `yaml:"ramping_likelihood"`  // 0.0-1.0
	Parameters         PhaseParameters `yaml:"parameters"`
}

type PhaseParameters struct {
	StartRPS IntRange `yaml:"start_rps"`
	EndRPS   IntRange `yaml:"end_rps"`
	Step     IntRange `yaml:"step"`
}

type IntRange struct {
	Min int `yaml:"min"`
	Max int `yaml:"max"`
}

func NewWorkloadPatternGenerator(seed int64, maxDuration time.Duration, patterns map[string]*PhasePattern) *WorkloadPatternGenerator {
	return &WorkloadPatternGenerator{
		seed:        seed,
		random:      rand.New(rand.NewPCG(uint64(seed), uint64(seed))),
		maxDuration: maxDuration,
		patterns:    patterns,
	}
}

// Generates a workload for the given patterns.
func (g *WorkloadPatternGenerator) GenerateWorkload() []TestPhase {
	workload := []TestPhase{}

	times := make(map[string]time.Duration)
	for _, pattern := range g.patterns {
		times[pattern.Endpoint] = time.Duration(0)
	}

	for _, pattern := range g.patterns {
		phaseCount := g.getRandInt(pattern.PhaseCount.Min, pattern.PhaseCount.Max)
		phaseDuration := g.maxDuration / time.Duration(phaseCount)

		for i := 0; i < phaseCount; i++ {
			phaseStartTime := times[pattern.Endpoint]
			times[pattern.Endpoint] += phaseDuration
			c := g.random.Float64()
			var phaseType string
			if c < pattern.ConstantLikelihood {
				phaseType = "constant"
			} else {
				phaseType = "variable"
			}

			phase := TestPhase{
				Name:      pattern.Endpoint + "_" + strconv.Itoa(i),
				Endpoint:  pattern.Endpoint,
				Type:      phaseType,
				StartTime: phaseStartTime,
				Duration:  phaseDuration,
				StartRPS:  g.getRandInt(pattern.Parameters.StartRPS.Min, pattern.Parameters.StartRPS.Max),
				EndRPS:    g.getRandInt(pattern.Parameters.EndRPS.Min, pattern.Parameters.EndRPS.Max),
				Step:      g.getRandInt(pattern.Parameters.Step.Min, pattern.Parameters.Step.Max),
			}
			workload = append(workload, phase)
		}
	}

	return workload
}

func (g *WorkloadPatternGenerator) getRandInt(min, max int) int {
	// Ensure min <= max by swapping if necessary
	if min > max {
		min, max = max, min
	}
	return g.random.IntN(max-min+1) + min
}
