//go:build !otel

package reliability

// OTelSink is a no-op metrics sink for builds without the "otel" tag. Emit
// drops snapshots so the reliability layer needs no telemetry dependency.
type OTelSink struct{}

// NewOTelSink returns a nil-safe no-op sink. The otel-tagged build takes a
// *sdkmetric.MeterProvider; the parameterless signature here keeps the default
// build free of any OpenTelemetry import.
func NewOTelSink() *OTelSink { return &OTelSink{} }

// Emit implements reliability.Sink by dropping the snapshot. Nil-safe.
func (s *OTelSink) Emit(Snapshot) {
	if s == nil {
		return
	}
}
