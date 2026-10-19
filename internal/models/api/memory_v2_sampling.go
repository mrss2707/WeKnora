package api

// HasTemperature distinguishes an explicit deterministic zero from historical
// callers whose zero value means "leave the provider default unchanged".
func (o *Options) HasTemperature() bool {
	return o != nil && (o.Temperature > 0 || (o.TemperatureSet && o.Temperature == 0))
}
