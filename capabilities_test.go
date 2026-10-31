package runnercore

import (
	"reflect"
	"testing"

	"go.uber.org/zap"

	"github.com/agynio/runner-core/catalog"
)

// A runner advertises hibernate exactly when its backend can: never from the
// catalog alone, since the Orchestrator would then ask and be refused.
func TestReportedCapabilitiesDerivesHibernateFromTheBackend(t *testing.T) {
	opts := Options{
		Catalog:      catalog.Catalog{Capabilities: []string{"gpu"}},
		Capabilities: []string{"docker"},
	}

	if got := reportedCapabilities(opts, true, zap.NewNop()); !reflect.DeepEqual(got, []string{"docker", "gpu", "hibernate"}) {
		t.Fatalf("expected [docker gpu hibernate], got %v", got)
	}
	if got := reportedCapabilities(opts, false, zap.NewNop()); !reflect.DeepEqual(got, []string{"docker", "gpu"}) {
		t.Fatalf("expected [docker gpu], got %v", got)
	}
}

func TestReportedCapabilitiesDropsADeclaredHibernateTheBackendCannotServe(t *testing.T) {
	opts := Options{Catalog: catalog.Catalog{Capabilities: []string{"hibernate", "docker"}}}

	if got := reportedCapabilities(opts, false, zap.NewNop()); !reflect.DeepEqual(got, []string{"docker"}) {
		t.Fatalf("expected [docker], got %v", got)
	}
}

// Nothing declared and nothing implemented reports nothing, not a nil that a
// caller might read as "unknown".
func TestReportedCapabilitiesReportsNothingWhenThereIsNothing(t *testing.T) {
	if got := reportedCapabilities(Options{}, false, zap.NewNop()); len(got) != 0 {
		t.Fatalf("expected no capabilities, got %v", got)
	}
}
