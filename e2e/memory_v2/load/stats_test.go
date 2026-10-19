package load

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPercentileNearestRankAndEmptyIsNaN(t *testing.T) {
	var durations []time.Duration
	for i := 1; i <= 100; i++ {
		durations = append(durations, time.Duration(i)*time.Millisecond)
	}
	require.Equal(t, 95.0, Percentile(durations, 95))
	require.Equal(t, 99.0, Percentile(durations, 99))
	require.True(t, math.IsNaN(Percentile(nil, 95)), "no measurement can never satisfy a threshold")
}

func TestSummarizeSeparatesExpectedPolicyFromFailures(t *testing.T) {
	summary := Summarize([]Sample{
		{Op: "read", Duration: time.Millisecond},
		{Op: "read", Duration: 2 * time.Millisecond},
		{Op: "read", Status: 500, Failed: true},
		{Op: "read", Status: 403, Expected: true},
	})["read"]
	require.Equal(t, 3, summary.Count)
	require.Equal(t, 1, summary.Failures)
	require.Equal(t, 1, summary.ExpectedPolicy)
	require.InDelta(t, 1.0/3.0, summary.FailureRate, 1e-9)
	require.Equal(t, 2.0, summary.P99Ms, "failed and policy samples are not latency samples")
}

func TestFixtureContentIsDistinctAndVectorValidationRejectsFakes(t *testing.T) {
	seen := map[string]bool{}
	for profile := 0; profile < 3; profile++ {
		for i := 0; i < 2000; i++ {
			content := FixtureContent(profile, i)
			require.False(t, seen[content])
			seen[content] = true
		}
	}
	require.Error(t, ValidateVector(make([]float32, 1024)))
	require.Error(t, ValidateVector(make([]float32, 8)))
	vector := make([]float32, 1024)
	vector[0] = float32(math.NaN())
	require.Error(t, ValidateVector(vector))
	vector[0] = 1
	require.NoError(t, ValidateVector(vector))
}
