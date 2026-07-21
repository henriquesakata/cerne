package domain

// CapabilityState is the lifecycle of a product-capability node (F-0188).
// Minimal, like notes: active until retired (superseded, no successor
// semantics needed — the taxonomy is curated in place).
type CapabilityState string

const (
	CapabilityActive     CapabilityState = "active"
	CapabilitySuperseded CapabilityState = "superseded"
)
