package handoff

import (
	"strings"
	"testing"
)

const openCodeExport = `{"info":{"id":"ses_1","title":"prova"},"messages":[
{"id":"m1","type":"user","text":"Aggiungi il login con Google","files":[],"agents":[]},
{"id":"m2","type":"assistant","content":[
  {"type":"reasoning","text":"penso..."},
  {"type":"text","text":"Parto dal controller."},
  {"type":"tool","name":"edit","state":{"status":"completed","input":{"filePath":"/r/app/auth.go"}}},
  {"type":"tool","name":"write","state":{"status":"completed","input":{"filePath":"/r/app/google.go"}}},
  {"type":"tool","name":"shell","state":{"status":"completed","input":{"command":"go test ./..."}}}
],"snapshot":{"files":["/r/app/auth.go"]}},
{"id":"m3","type":"idle","outcome":"failed"},
{"id":"m4","type":"model-switched"},
{"id":"m5","type":"user","text":"ora i test"},
{"id":"m6","type":"assistant","content":[{"type":"text","text":"Test scritti, due falliscono."}]}
]}`

func TestSummaryOpenCode(t *testing.T) {
	s, err := SummaryOpenCode([]byte(openCodeExport), "/tmp/ses_1.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Aggiungi il login con Google", "ora i test", "Test scritti, due falliscono.", "/r/app/auth.go", "/r/app/google.go", "/tmp/ses_1.json"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary misses %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "penso...") || strings.Contains(s, "go test") {
		t.Errorf("reasoning and commands stay out:\n%s", s)
	}
	if strings.Count(s, "/r/app/auth.go") != 1 {
		t.Errorf("each file once:\n%s", s)
	}
	if _, err := SummaryOpenCode([]byte(`{"messages":[]}`), "x"); err == nil {
		t.Errorf("a session without requests is an error")
	}
}
