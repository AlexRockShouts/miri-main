package tools

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestPinchTabTool_Info(t *testing.T) {
	tmp := t.TempDir()
	pt := NewPinchTabTool(tmp)
	info, err := pt.Info(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "pinchtab_browser", info.Name)
	assert.NotEmpty(t, info.Desc)
}

func TestInvokableRun_ParseArgs(t *testing.T) {
	tmp := t.TempDir()
	pt := NewPinchTabTool(tmp)
	// mock server not needed for parse
	argsJSON := `{"command": "nav https://example.com"}`
	res, err := pt.InvokableRun(context.Background(), argsJSON)
	assert.NoError(t, err)
	assert.Contains(t, res, "stdout")
	var resMap map[string]interface{}
	assert.NoError(t, json.Unmarshal([]byte(res), &resMap))
}
