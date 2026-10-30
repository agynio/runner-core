// Package catalog is what a runner offers: the compute sizes, storage tiers
// and capabilities it can honour, declared in the runner's own configuration
// and reported to the platform at startup.
//
// It is declared next to the runner rather than in the platform because every
// entry needs a runner-side implementation anyway — a flavor is only real if
// the runner can allocate it, and a storage class is only real if something
// backs it. What backs an entry is the runner's own business and stays in its
// own catalog type; this package holds the part the platform sees.
package catalog

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	runnersv1 "github.com/agynio/runner-core/gen/agynio/api/runners/v1"
)

// CapabilityHibernate is advertised by a runner whose backend can hibernate
// and wake workloads. The runner front derives it from the backend rather
// than taking it from the declared catalog, so a runner never claims it by
// configuration alone.
const CapabilityHibernate = "hibernate"

// Catalog is the platform-facing view of what a runner offers.
type Catalog struct {
	Flavors        []Flavor       `yaml:"flavors"`
	StorageClasses []StorageClass `yaml:"storageClasses"`
	Capabilities   []string       `yaml:"capabilities"`
}

// Flavor is a named compute size. Resources is what the platform stores and
// shows; how a runner spreads it across what it runs is its own business.
type Flavor struct {
	Name       string           `yaml:"name"`
	Default    bool             `yaml:"default"`
	Deprecated bool             `yaml:"deprecated"`
	Resources  ComputeResources `yaml:"resources"`
}

// ComputeResources is a size as the platform understands it.
type ComputeResources struct {
	RequestsCPU    string `yaml:"requestsCpu"`
	RequestsMemory string `yaml:"requestsMemory"`
	LimitsCPU      string `yaml:"limitsCpu"`
	LimitsMemory   string `yaml:"limitsMemory"`
}

// IsZero reports whether nothing at all was declared.
func (r ComputeResources) IsZero() bool {
	for _, field := range r.fields() {
		if strings.TrimSpace(field.value) != "" {
			return false
		}
	}
	return true
}

// MissingField names the first field left empty, in declaration order, so a
// half-declared size is rejected with the field to fill in.
func (r ComputeResources) MissingField() (string, bool) {
	for _, field := range r.fields() {
		if strings.TrimSpace(field.value) == "" {
			return field.name, true
		}
	}
	return "", false
}

type namedField struct {
	name  string
	value string
}

func (r ComputeResources) fields() []namedField {
	return []namedField{
		{name: "requestsCpu", value: r.RequestsCPU},
		{name: "requestsMemory", value: r.RequestsMemory},
		{name: "limitsCpu", value: r.LimitsCPU},
		{name: "limitsMemory", value: r.LimitsMemory},
	}
}

// StorageClass is a named storage tier. What backs it stays with the runner.
type StorageClass struct {
	Name       string `yaml:"name"`
	Default    bool   `yaml:"default"`
	Deprecated bool   `yaml:"deprecated"`
}

// LoadFile reads the catalog declared at path into the runner's own catalog
// type, which may carry more than the platform sees. A missing path is not an
// error: a runner with no declared catalog reports nothing and simply offers
// no named entries.
func LoadFile(path string, into any) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read catalog %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, into); err != nil {
		return fmt.Errorf("parse catalog %s: %w", path, err)
	}
	return nil
}

// Load reads a catalog declared in this package's own shape and validates it.
func Load(path string) (Catalog, error) {
	var catalog Catalog
	if err := LoadFile(path, &catalog); err != nil {
		return Catalog{}, err
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, fmt.Errorf("catalog %s: %w", strings.TrimSpace(path), err)
	}
	return catalog, nil
}

// Validate catches locally what the platform would reject anyway, so a bad
// configuration fails at startup with a clear message instead of as a rejected
// report.
func (c Catalog) Validate() error {
	flavorNames := map[string]struct{}{}
	defaults := 0
	for _, flavor := range c.Flavors {
		name := strings.TrimSpace(flavor.Name)
		if name == "" {
			return fmt.Errorf("flavor name is empty")
		}
		if _, exists := flavorNames[name]; exists {
			return fmt.Errorf("flavor %q is declared more than once", name)
		}
		flavorNames[name] = struct{}{}
		if flavor.Default {
			defaults++
		}
		if field, missing := flavor.Resources.MissingField(); missing {
			return fmt.Errorf("flavor %q: %s is empty", name, field)
		}
	}
	if defaults > 1 {
		return fmt.Errorf("at most one flavor may be default, got %d", defaults)
	}

	classNames := map[string]struct{}{}
	classDefaults := 0
	for _, class := range c.StorageClasses {
		name := strings.TrimSpace(class.Name)
		if name == "" {
			return fmt.Errorf("storage class name is empty")
		}
		if _, exists := classNames[name]; exists {
			return fmt.Errorf("storage class %q is declared more than once", name)
		}
		classNames[name] = struct{}{}
		if class.Default {
			classDefaults++
		}
	}
	if classDefaults > 1 {
		return fmt.Errorf("at most one storage class may be default, got %d", classDefaults)
	}
	return nil
}

// Capabilities is what a runner reports it can do: what its catalog declares,
// merged with what it has an implementation for.
//
// The catalog once listed capabilities by hand while the runner decided what
// it could serve from its configured implementations, and the two silently
// disagreed: docker was configured and working, the catalog said nothing, and
// the Orchestrator placed no agent that asked for it -- "no eligible runners
// found (required capabilities: [docker])" -- with the runner right there
// able to serve them.
//
// The declared entries are still honoured, so a capability can be advertised
// ahead of the implementation landing, but they no longer have to be kept in
// step by hand for the ones the runner already implements.
func Capabilities(declared []string, implemented ...string) []string {
	capabilities := make([]string, 0, len(declared)+len(implemented))
	seen := map[string]struct{}{}
	add := func(capability string) {
		if capability == "" {
			return
		}
		if _, ok := seen[capability]; ok {
			return
		}
		seen[capability] = struct{}{}
		capabilities = append(capabilities, capability)
	}
	for _, capability := range declared {
		add(strings.TrimSpace(capability))
	}
	for _, capability := range implemented {
		add(strings.TrimSpace(capability))
	}
	sort.Strings(capabilities)
	return capabilities
}

// Report renders the catalog into the report the platform stores, advertising
// capabilities as given -- see Capabilities for how a runner arrives at them.
// The platform only needs the names and sizes; what backs each entry stays
// with the runner.
func (c Catalog) Report(serviceToken string, capabilities []string) *runnersv1.ReportRunnerCatalogRequest {
	flavors := make([]*runnersv1.FlavorEntry, 0, len(c.Flavors))
	for _, flavor := range c.Flavors {
		flavors = append(flavors, &runnersv1.FlavorEntry{
			Name:       flavor.Name,
			Default:    flavor.Default,
			Deprecated: flavor.Deprecated,
			Resources: &runnersv1.ComputeResources{
				RequestsCpu:    flavor.Resources.RequestsCPU,
				RequestsMemory: flavor.Resources.RequestsMemory,
				LimitsCpu:      flavor.Resources.LimitsCPU,
				LimitsMemory:   flavor.Resources.LimitsMemory,
			},
		})
	}
	storageClasses := make([]*runnersv1.StorageClassEntry, 0, len(c.StorageClasses))
	for _, class := range c.StorageClasses {
		storageClasses = append(storageClasses, &runnersv1.StorageClassEntry{
			Name:       class.Name,
			Default:    class.Default,
			Deprecated: class.Deprecated,
		})
	}
	return &runnersv1.ReportRunnerCatalogRequest{
		ServiceToken:   serviceToken,
		Flavors:        flavors,
		StorageClasses: storageClasses,
		Capabilities:   capabilities,
	}
}
