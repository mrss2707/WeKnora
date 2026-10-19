package api

import (
	"math"
	"testing"
)

func TestExplicitZeroSamplingPreservesLegacyDefaults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options *Options
		want    bool
	}{
		{"nil", nil, false}, {"legacy zero", &Options{}, false},
		{"explicit zero", &Options{TemperatureSet: true}, true},
		{"positive", &Options{Temperature: .2}, true},
		{"negative", &Options{Temperature: -1, TemperatureSet: true}, false},
		{"nan", &Options{Temperature: math.NaN(), TemperatureSet: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.options.HasTemperature(); got != tc.want {
				t.Fatalf("HasTemperature=%t, want %t", got, tc.want)
			}
		})
	}
}
