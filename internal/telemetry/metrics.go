package telemetry

// observeOp feeds a finished record into the process metrics. Filled by the
// Prometheus layer; a no-op until then.
func observeOp(Record) {}
