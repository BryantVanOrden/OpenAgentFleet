package protocol

import "testing"

// The last injection in this system came from assuming a tool name was well
// behaved, so names and specs are validated against an allowlist rather than
// filtered for known-bad characters.
func TestCustomToolRejectsShellMetacharacters(t *testing.T) {
	bad := []CustomTool{
		{Name: "$(id)", Method: "apt", Spec: "jq"},
		{Name: "a;id", Method: "apt", Spec: "jq"},
		{Name: "a`id`", Method: "apt", Spec: "jq"},
		{Name: "a b", Method: "apt", Spec: "jq"},
		{Name: "../../etc/passwd", Method: "apt", Spec: "jq"},
		{Name: "ok", Method: "apt", Spec: "$(id)"},
		{Name: "ok", Method: "apt", Spec: "pkg;id"},
		{Name: "ok", Method: "apt", Spec: "pkg`id`"},
		{Name: "ok", Method: "apt", Spec: "pkg with space"},
		{Name: "ok", Method: "apt", Spec: "pkg'quote"},
		{Name: "", Method: "apt", Spec: "jq"},
		{Name: "ok", Method: "eval", Spec: "anything"},
		{Name: "ok", Method: "github", Spec: "no-slash"},
	}
	for _, c := range bad {
		if err := c.Validate(); err == nil {
			t.Errorf("accepted a dangerous tool: %+v", c)
		}
	}
}

func TestCustomToolAcceptsRealOnes(t *testing.T) {
	good := []CustomTool{
		{Name: "kubectl", Method: "apt", Spec: "kubectl"},
		{Name: "aws-cli", Method: "apt", Spec: "awscli"},
		{Name: "pandas", Method: "pip", Spec: "pandas"},
		{Name: "pdfminer", Method: "pip", Spec: "pdfminer.six"},
		{Name: "cypress", Method: "npm", Spec: "cypress"},
		{Name: "lhci", Method: "npm", Spec: "@lhci/cli"},
		{Name: "nuclei", Method: "go", Spec: "github.com/projectdiscovery/nuclei/v3/cmd/nuclei@latest"},
		{Name: "yq", Method: "url", Spec: "https://github.com/mikefarah/yq/releases/latest/download/yq_linux_amd64"},
		{Name: "seclists", Method: "github", Spec: "danielmiessler/SecLists"},
		{Name: "tool_1.2+x", Method: "apt", Spec: "pkg"},
	}
	for _, c := range good {
		if err := c.Validate(); err != nil {
			t.Errorf("rejected a legitimate tool %+v: %v", c, err)
		}
	}
}

// A name long enough to be a payload rather than a name is refused.
func TestCustomToolLengthIsBounded(t *testing.T) {
	long := ""
	for i := 0; i < 200; i++ {
		long += "a"
	}
	if err := (CustomTool{Name: long, Method: "apt", Spec: "jq"}).Validate(); err == nil {
		t.Error("accepted an unbounded tool name")
	}
}
