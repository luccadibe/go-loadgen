package go_loadgen

import (
	"errors"
	"math/rand/v2"
	"strconv"
	"time"
)

type WorkloadPatternGenerator struct {
	seed        int64
	random      *rand.Rand
	maxDuration time.Duration
	patterns    []*PhasePattern
}

// PhasePattern is a template for a workload phase. It is used to generate a workload of TestPhases.
type PhasePattern struct {
	Name               string   `yaml:"name"`
	PhaseCount         IntRange `yaml:"phase_count"`
	ConstantLikelihood float64  `yaml:"constant_likelihood"` // 0.0-1.0
	RampingLikelihood  float64  `yaml:"ramping_likelihood"`  // 0.0-1.0
	// What percentage of the total workload time this pattern should take up. 0.0-1.0
	// Note: all patterns must sum to 1.0. If not provided, all patterns will be weighted equally.
	Weight float64 `yaml:"weight"`
	// Knobs to affect the generated workload phase
	Parameters PhaseParameters `yaml:"parameters"`
	// how much time this patter will take up
	totalTime time.Duration
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

func NewWorkloadPatternGenerator(seed int64, maxDuration time.Duration, patterns []*PhasePattern) *WorkloadPatternGenerator {
	return &WorkloadPatternGenerator{
		seed:        seed,
		random:      rand.New(rand.NewPCG(uint64(seed), uint64(seed))),
		maxDuration: maxDuration,
		patterns:    patterns,
	}
}

// Generates a workload for the given patterns.
func (g *WorkloadPatternGenerator) GenerateWorkload() ([]TestPhase, error) {
	workload := []TestPhase{}

	currentTime := time.Duration(0)

	// calculate times for each pattern
	if err := g.calculatePatternTimes(); err != nil {
		return nil, err
	}

	for _, pattern := range g.patterns {
		phaseCount := g.getRandInt(pattern.PhaseCount.Min, pattern.PhaseCount.Max)
		phaseDuration := pattern.totalTime / time.Duration(phaseCount)

		for i := 0; i < phaseCount; i++ {
			phaseStartTime := currentTime
			currentTime += phaseDuration
			c := g.random.Float64()
			var phaseType string
			if c < pattern.ConstantLikelihood {
				phaseType = "constant"
			} else {
				phaseType = "variable"
			}

			phase := TestPhase{
				Name:      pattern.Name + "_" + strconv.Itoa(i),
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

	return workload, nil
}

func (g *WorkloadPatternGenerator) getRandInt(min, max int) int {
	// Ensure min <= max by swapping if necessary
	if min > max {
		min, max = max, min
	}
	return g.random.IntN(max-min+1) + min
}

func (g *WorkloadPatternGenerator) calculatePatternTimes() error {
	if len(g.patterns) == 0 {
		return nil
	}

	total := 0.0
	for _, pattern := range g.patterns {
		total += pattern.Weight
	}
	// Not provided, split workload evenly between all patterns
	if total == 0.0 {
		equalTime := g.maxDuration / time.Duration(len(g.patterns))
		for _, pattern := range g.patterns {
			pattern.totalTime = equalTime
		}
		return nil
	}
	if total != 1.0 {
		return errors.New("pattern weights must sum to 1.0")
	}
	for _, pattern := range g.patterns {
		pattern.totalTime = time.Duration(float64(g.maxDuration) * pattern.Weight)
	}
	return nil
}
