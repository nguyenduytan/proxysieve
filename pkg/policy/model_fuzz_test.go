package policy

import (
	"encoding/json"
	"testing"
)

func FuzzPolicyJSON(f *testing.F) {
	f.Add([]byte(`{"version":1,"id":"policy","name":"Policy","rules":[{"id":"rule","name":"Rule","enabled":true,"stop_processing":true,"actions":[{"type":"direct"}]}]}`))
	f.Add([]byte(`{"version":1,"rules":[]}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			t.Skip()
		}
		var document Policy
		if json.Unmarshal(data, &document) == nil && document.Validate() == nil && document.Clone().Validate() != nil {
			t.Fatal("valid policy became invalid after clone")
		}
	})
}
