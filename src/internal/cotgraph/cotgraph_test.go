package cotgraph

import (
	"context"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantNodes int
		wantEdges int
	}{
		{
			name:      "basic DRE",
			input:     "[D]: Deduce\n[R]: Reflect\n[E]: Execute",
			wantNodes: 3,
			wantEdges: 2,
		},
		{
			name:      "Thoughts",
			input:     "[Thought: First]\n[Thought: Second]",
			wantNodes: 2,
			wantEdges: 1,
		},
		{
			name:      "mixed",
			input:     "[Thought: Start]\n[D]: Dream\n[R]: Reason\n[E]: Act",
			wantNodes: 4,
			wantEdges: 3,
		},
		{
			name:      "empty",
			input:     "",
			wantNodes: 0,
			wantEdges: 0,
		},
		{
			name:      "malformed",
			input:     "Plain text\n[Invalid]",
			wantNodes: 0,
			wantEdges: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := Parse(tt.input)
			if err != nil {
				t.Errorf("Parse() error = %v", err)
				return
			}
			if len(g.Nodes) != tt.wantNodes {
				t.Errorf("Parse() nodes = %d, want %d", len(g.Nodes), tt.wantNodes)
			}
			if len(g.Edges) != tt.wantEdges {
				t.Errorf("Parse() edges = %d, want %d", len(g.Edges), tt.wantEdges)
			}
		})
	}
}

func TestDetectCycles(t *testing.T) {
	input := "[D]: A\n[R]: B\n[E]: A" // texts repeat, but no structural cycle
	g, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	r := g.DetectCycles()
	if r.HasCycle {
		t.Error("Should no cycle in linear")
	}

	// Note: current impl detects structural cycles only; semantic repeat not cycle
}

func TestAnalyze(t *testing.T) {
	input := "[D]: Plan\n[R]: Check"
	_, err := Analyze(context.Background(), input)
	if err != nil {
		t.Errorf("Analyze() error = %v", err)
	}
	// Check json valid
}
