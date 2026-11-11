package store

import (
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
)

// Capability is the persisted form of a product-capability node (F-0188): the
// functional tree of the product (e.g. Studio → Projetos → Issues), a separate
// axis from modules (product area) and epics (work decomposition). Parent,
// when set, is the capability this one refines; features attach through
// Feature.Capabilities.
type Capability struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	Parent    string                 `json:"parent,omitempty"`
	State     domain.CapabilityState `json:"state"`
	Reason    string                 `json:"reason,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// SaveCapability writes the capability to entities/capabilities/<ID>.json.
func SaveCapability(backendDir string, c Capability) error {
	return saveEntity(backendDir, capabilitiesSubdir, c.ID, c)
}

// LoadCapability reads one capability by id; ErrNotFound when missing.
func LoadCapability(backendDir, id string) (Capability, error) {
	return loadEntity[Capability](backendDir, capabilitiesSubdir, id)
}

// LoadAllCapabilities returns all capabilities sorted by ID ascending.
func LoadAllCapabilities(backendDir string) ([]Capability, error) {
	return loadAllEntities[Capability](backendDir, capabilitiesSubdir)
}
