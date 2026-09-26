package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDataHoldsOnlyDomainFields pins the split issue #66 asks for: the domain
// model carries config.yaml's sections and nothing else. A persistence detail
// or a test seam added back to it would make YAML field order, field
// alignment and load context one struct's problem again.
func TestDataHoldsOnlyDomainFields(t *testing.T) {
	typ := reflect.TypeOf(Data{})

	for i := range typ.NumField() {
		field := typ.Field(i)
		assert.True(t, field.IsExported(), "Data.%s is unexported: persistence state belongs on Config", field.Name)
		assert.NotEmpty(t, field.Tag.Get("yaml"), "Data.%s has no yaml tag, so it is not a config.yaml section", field.Name)
	}
}

// TestConfigKeepsPersistenceContextOutOfTheDomainModel is the other half: the
// wrapper is what knows where the config came from.
func TestConfigKeepsPersistenceContextOutOfTheDomainModel(t *testing.T) {
	typ := reflect.TypeOf(Config{})

	dataField, ok := typ.FieldByName("Data")
	require.True(t, ok, "Config must embed the domain model")
	assert.Equal(t, ",inline", dataField.Tag.Get("yaml"), "Data must stay inline so config.yaml's shape is unchanged")

	var unexported []string
	for i := range typ.NumField() {
		if field := typ.Field(i); !field.IsExported() {
			unexported = append(unexported, field.Name)
		}
	}
	assert.NotEmpty(t, unexported, "Config is the wrapper that holds the persistence context")
}

// TestWritePersistsEveryDomainSection covers the drift the duplicated field
// list in write() invited: a section added to the domain model but forgotten
// in the writer's own copy of it would have been dropped on every save.
func TestWritePersistsEveryDomainSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Default()
	cfg.filePath = path
	cfg.SSHConnections = map[string]SSHConnection{"host1": {Host: "example.test", User: "demo"}}
	cfg.CommandCenter = CommandCenterConfig{Enabled: true}
	cfg.TaskDashboard = TaskDashboardConfig{Summary: TaskSummaryConfig{Enabled: true}}
	shim := false
	cfg.URLOpen = URLOpenConfig{BrowserShim: &shim}

	require.NoError(t, cfg.write())

	written, err := os.ReadFile(path)
	require.NoError(t, err)

	typ := reflect.TypeOf(Data{})
	for i := range typ.NumField() {
		key := strings.Split(typ.Field(i).Tag.Get("yaml"), ",")[0]
		if key == "layout" {
			// The top-level layout mirrors the active workspace's layout for
			// in-memory readers; workspaces own the persisted copy.
			assert.NotContains(t, string(written), "\nlayout:", "the legacy top-level layout must not be written back")
			continue
		}
		assert.Contains(t, string(written), key+":", "section %q was not persisted", key)
	}
}

// TestWriteKeepsSectionOrderStable pins the user-facing serialization order,
// which is the reason the domain model is exempt from fieldalignment.
func TestWriteKeepsSectionOrderStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Default()
	cfg.filePath = path
	cfg.SSHConnections = map[string]SSHConnection{"host1": {Host: "example.test", User: "demo"}}
	cfg.Display = DisplayConfig{ShowHeader: true}
	cfg.CommandCenter = CommandCenterConfig{Enabled: true}

	require.NoError(t, cfg.write())

	written, err := os.ReadFile(path)
	require.NoError(t, err)

	var order []int
	sections := []string{
		"server:", "ssh_connections:", "workspaces:", "display:", "agent_board:", "command_center:",
	}
	for _, key := range sections {
		idx := strings.Index(string(written), "\n"+key)
		if strings.HasPrefix(string(written), key) {
			idx = 0
		}
		require.GreaterOrEqual(t, idx, 0, "section %q missing from written config", key)
		order = append(order, idx)
	}
	assert.IsIncreasing(t, order)
}
