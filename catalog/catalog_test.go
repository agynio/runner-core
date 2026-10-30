package catalog

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeCatalog(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	return path
}

func TestLoadReadsDeclaredEntries(t *testing.T) {
	path := writeCatalog(t, `
flavors:
  - name: ram-2gb
    default: true
    resources:
      requestsCpu: "500m"
      requestsMemory: "2Gi"
      limitsCpu: "2"
      limitsMemory: "2Gi"
  - name: ram-4gb
    resources:
      requestsCpu: "1"
      requestsMemory: "4Gi"
      limitsCpu: "4"
      limitsMemory: "4Gi"
storageClasses:
  - name: default
    default: true
  - name: fast-ssd
capabilities: [docker]
`)

	catalog, err := Load(path)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	if len(catalog.Flavors) != 2 {
		t.Fatalf("expected 2 flavors, got %d", len(catalog.Flavors))
	}
	if !catalog.Flavors[0].Default || catalog.Flavors[0].Name != "ram-2gb" {
		t.Fatalf("expected ram-2gb to be the default flavor, got %+v", catalog.Flavors[0])
	}
	if catalog.Flavors[1].Resources.RequestsMemory != "4Gi" {
		t.Fatalf("expected 4Gi request, got %q", catalog.Flavors[1].Resources.RequestsMemory)
	}
	if len(catalog.StorageClasses) != 2 || !catalog.StorageClasses[0].Default {
		t.Fatalf("expected two storage classes with the first default, got %+v", catalog.StorageClasses)
	}
	if len(catalog.Capabilities) != 1 || catalog.Capabilities[0] != "docker" {
		t.Fatalf("expected docker capability, got %v", catalog.Capabilities)
	}
}

// A runner's own catalog type may carry what backs an entry; the loader hands
// the file to that type and leaves what it does not know alone.
func TestLoadFileFillsTheRunnersOwnType(t *testing.T) {
	type flavor struct {
		Flavor `yaml:",inline"`
		VCPUs  int `yaml:"vcpus"`
	}
	type ownCatalog struct {
		Flavors []flavor `yaml:"flavors"`
	}
	path := writeCatalog(t, `
flavors:
  - name: small
    vcpus: 2
    resources: {requestsCpu: "2", requestsMemory: 2Gi, limitsCpu: "2", limitsMemory: 2Gi}
`)

	var own ownCatalog
	if err := LoadFile(path, &own); err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(own.Flavors) != 1 || own.Flavors[0].Name != "small" || own.Flavors[0].VCPUs != 2 {
		t.Fatalf("unexpected catalog %+v", own)
	}
}

func TestLoadTreatsAbsentFileAsEmpty(t *testing.T) {
	// A runner may run without a declared catalog; that is not a failure.
	catalog, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("load missing catalog: %v", err)
	}
	if len(catalog.Flavors) != 0 {
		t.Fatalf("expected empty catalog, got %+v", catalog)
	}

	if _, err := Load(""); err != nil {
		t.Fatalf("load unset catalog path: %v", err)
	}
}

func TestLoadRejectsUnparseableFile(t *testing.T) {
	path := writeCatalog(t, "flavors: [")

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "parse catalog") {
		t.Fatalf("expected a parse error, got %v", err)
	}
}

func TestLoadRejectsSecondDefault(t *testing.T) {
	path := writeCatalog(t, `
flavors:
  - name: ram-2gb
    default: true
    resources: {requestsCpu: "1", requestsMemory: 2Gi, limitsCpu: "1", limitsMemory: 2Gi}
  - name: ram-4gb
    default: true
    resources: {requestsCpu: "1", requestsMemory: 4Gi, limitsCpu: "1", limitsMemory: 4Gi}
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "at most one flavor") {
		t.Fatalf("expected single-default rejection, got %v", err)
	}
}

func TestLoadRejectsIncompleteResources(t *testing.T) {
	path := writeCatalog(t, `
flavors:
  - name: ram-2gb
    resources: {requestsCpu: "1", requestsMemory: 2Gi, limitsCpu: "1"}
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "limitsMemory") {
		t.Fatalf("expected missing limitsMemory to be rejected, got %v", err)
	}
}

func TestLoadRejectsDuplicateNames(t *testing.T) {
	path := writeCatalog(t, `
flavors:
  - name: ram-2gb
    resources: {requestsCpu: "1", requestsMemory: 2Gi, limitsCpu: "1", limitsMemory: 2Gi}
  - name: ram-2gb
    resources: {requestsCpu: "2", requestsMemory: 2Gi, limitsCpu: "2", limitsMemory: 2Gi}
`)

	if _, err := Load(path); err == nil {
		t.Fatal("expected duplicate flavor name to be rejected")
	}
}

func TestValidateRejectsSecondDefaultStorageClass(t *testing.T) {
	catalog := Catalog{StorageClasses: []StorageClass{
		{Name: "a", Default: true},
		{Name: "b", Default: true},
	}}

	err := catalog.Validate()
	if err == nil || !strings.Contains(err.Error(), "at most one storage class") {
		t.Fatalf("expected single-default rejection, got %v", err)
	}
}

func TestMissingFieldNamesTheFirstGap(t *testing.T) {
	if field, missing := (ComputeResources{RequestsCPU: "1", RequestsMemory: "1Gi", LimitsCPU: "1", LimitsMemory: "1Gi"}).MissingField(); missing {
		t.Fatalf("expected a complete size to miss nothing, got %q", field)
	}
	if field, missing := (ComputeResources{RequestsCPU: "1", LimitsMemory: "1Gi"}).MissingField(); !missing || field != "requestsMemory" {
		t.Fatalf("expected requestsMemory to be missing, got %q (missing=%v)", field, missing)
	}
	if !(ComputeResources{}).IsZero() {
		t.Fatal("expected an undeclared size to be zero")
	}
}

// The catalog listed capabilities by hand while the runner decided what it
// could serve from its configured implementations, and the two disagreed:
// docker was configured and working, the catalog said nothing, and the
// Orchestrator refused to place any agent that asked for it -- "no eligible
// runners found (required capabilities: [docker])".
func TestCapabilitiesReportsWhatIsImplemented(t *testing.T) {
	if got := Capabilities(nil, "docker"); !reflect.DeepEqual(got, []string{"docker"}) {
		t.Fatalf("expected [docker], got %v", got)
	}
}

// Without an implementation the runner cannot serve it, so it must not claim it.
func TestCapabilitiesOmitsWhatIsNotImplemented(t *testing.T) {
	if got := Capabilities(nil); len(got) != 0 {
		t.Fatalf("expected no capabilities, got %v", got)
	}
}

// The catalog entry still stands on its own, so an operator can advertise
// something ahead of the implementation, and listing docker there as well does
// not report it twice.
func TestCapabilitiesMergesTheCatalogEntry(t *testing.T) {
	got := Capabilities([]string{"docker", " gpu "}, "docker")
	if !reflect.DeepEqual(got, []string{"docker", "gpu"}) {
		t.Fatalf("expected [docker gpu], got %v", got)
	}
}

func TestReportCarriesNamesSizesAndCapabilities(t *testing.T) {
	catalog := Catalog{
		Flavors: []Flavor{{
			Name: "ram-2gb", Default: true,
			Resources: ComputeResources{RequestsCPU: "500m", RequestsMemory: "2Gi", LimitsCPU: "2", LimitsMemory: "2Gi"},
		}},
		StorageClasses: []StorageClass{{Name: "fast-ssd", Deprecated: true}},
	}

	report := catalog.Report("token", []string{"docker"})
	if report.ServiceToken != "token" {
		t.Fatalf("expected the service token, got %q", report.ServiceToken)
	}
	if len(report.Flavors) != 1 || report.Flavors[0].Name != "ram-2gb" || !report.Flavors[0].Default {
		t.Fatalf("unexpected flavors %+v", report.Flavors)
	}
	if report.Flavors[0].Resources.GetLimitsMemory() != "2Gi" || report.Flavors[0].Resources.GetRequestsCpu() != "500m" {
		t.Fatalf("unexpected resources %+v", report.Flavors[0].Resources)
	}
	if len(report.StorageClasses) != 1 || report.StorageClasses[0].Name != "fast-ssd" || !report.StorageClasses[0].Deprecated {
		t.Fatalf("unexpected storage classes %+v", report.StorageClasses)
	}
	if !reflect.DeepEqual(report.Capabilities, []string{"docker"}) {
		t.Fatalf("unexpected capabilities %v", report.Capabilities)
	}
}
