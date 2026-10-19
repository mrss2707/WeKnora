package workers

import "time"

// Model clients carry no HTTP timeout of their own, so a hung hosted provider
// would otherwise hold a Dreamer scope lock or the extraction batch forever.
// The deadlines are generous for reasoning models; a timed-out Dreamer call
// leaves its token reservation charged and the scope is retried next pass.
const (
	dreamerModelTimeout   = 5 * time.Minute
	extractorModelTimeout = 3 * time.Minute
)
