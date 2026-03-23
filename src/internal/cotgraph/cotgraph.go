package cotgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type NodeType string

const (
	NodeD NodeType = "D" // Dream/Deep/Deduce
	NodeR NodeType = "R" // Reason/Reflect
	NodeE NodeType = "E" // Execute/Explore
	NodeT NodeType = "T" // Thought
)

type Node struct {
	ID   string   `json:"id"`
	Type NodeType `json:"type"`
	Text string   `json:"text"`
}

type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
}

type Graph struct {
	Nodes map[string]Node `json:"nodes"`
	Edges []Edge          `json:"edges"`
}

type Report struct {
	HasCycle bool       `json:"has_cycle"`
	Cycles   [][]string `json:"cycles,omitempty"`
	Summary  string     `json:"summary"`
}

func Parse(input string) (*Graph, error) {
	g := &Graph{
		Nodes: make(map[string]Node),
	}

	// Line by line parsing for [D]:, [R]:, [E]:, [Thought:]
	lines := strings.Split(input, "\n")

	var nodeID int
	prevID := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var nodeType NodeType
		var text string

		if strings.HasPrefix(line, "[D]:") || strings.HasPrefix(line, "[D] ") {
			nodeType = NodeD
			text = strings.TrimPrefix(strings.TrimPrefix(line, "[D]:"), ":")
			text = strings.TrimSpace(text)
		} else if strings.HasPrefix(line, "[R]:") || strings.HasPrefix(line, "[R] ") {
			nodeType = NodeR
			text = strings.TrimPrefix(strings.TrimPrefix(line, "[R]:"), ":")
			text = strings.TrimSpace(text)
		} else if strings.HasPrefix(line, "[E]:") || strings.HasPrefix(line, "[E] ") {
			nodeType = NodeE
			text = strings.TrimPrefix(strings.TrimPrefix(line, "[E]:"), ":")
			text = strings.TrimSpace(text)
		} else if strings.HasPrefix(line, "[Thought:") {
			nodeType = NodeT
			text = strings.TrimPrefix(line, "[Thought:")
			text = strings.TrimSpace(strings.TrimRight(text, "]"))
		} else {
			continue
		}

		id := fmt.Sprintf("n%d", nodeID)
		nodeID++

		g.Nodes[id] = Node{ID: id, Type: nodeType, Text: text}

		if prevID != "" {
			g.Edges = append(g.Edges, Edge{From: prevID, To: id, Label: string(nodeType)})
		}
		prevID = id
	}

	return g, nil
}

func (g *Graph) DetectCycles() Report {
	// Simple cycle detection for directed graph using DFS
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	var cycles [][]string

	var dfs func(string, []string) bool
	dfs = func(nodeID string, path []string) bool {
		if recStack[nodeID] {
			// Cycle found
			cycle := make([]string, len(path), len(path)+1)
			copy(cycle, path)
			cycle = append(cycle, g.Nodes[nodeID].Text)
			cycles = append(cycles, cycle)
			return true
		}
		if visited[nodeID] {
			return false
		}
		visited[nodeID] = true
		recStack[nodeID] = true

		path = append(path, g.Nodes[nodeID].Text)

		for _, edge := range g.Edges {
			if edge.From == nodeID {
				if dfs(edge.To, path) {
					return true
				}
			}
		}

		delete(recStack, nodeID)
		return false
	}

	for id := range g.Nodes {
		if !visited[id] {
			dfs(id, nil)
		}
	}

	hasCycle := len(cycles) > 0
	summary := fmt.Sprintf("Graph: %d nodes, %d edges. Cycles: %d", len(g.Nodes), len(g.Edges), len(cycles))

	return Report{HasCycle: hasCycle, Cycles: cycles, Summary: summary}
}

func Analyze(_ context.Context, input string) (string, error) {
	g, err := Parse(input)
	if err != nil {
		return "", err
	}
	r := g.DetectCycles()
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b), nil
}
