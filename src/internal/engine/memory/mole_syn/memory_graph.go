package mole_syn

import (
	"fmt"
	"log/slog"
	"miri-main/src/internal/storage"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/model"
	"github.com/dominikbraun/graph"
	"github.com/google/uuid"
)

type BondType string

const (
	Deep    BondType = "deep"
	Reflect BondType = "reflect"
	Explore BondType = "explore"
)

type Node struct {
	ID      string         `json:"id"`
	Content string         `json:"content"`
	Meta    map[string]any `json:"meta,omitempty"`
}

type EdgeData struct {
	Bond BondType `json:"bond"`
}

type MemoryGraph struct {
	g          graph.Graph[string, string]   // we don't use the V type meaningfully
	vertexData map[string]Node               // ← IMPORTANT: store your Node here
	lastNode   map[string]string             // sessionID → last node ID
	edgeData   map[string]EdgeData           // key = "from->to"
	transCount map[BondType]map[BondType]int // from-bond → to-bond → count
	mu         sync.RWMutex
	chat       model.BaseChatModel
	st         *storage.Storage
}

func New(chat model.BaseChatModel, st *storage.Storage) *MemoryGraph {
	g := graph.New(graph.StringHash, graph.Directed())
	mg := &MemoryGraph{
		g:          g,
		vertexData: make(map[string]Node),
		lastNode:   make(map[string]string),
		edgeData:   make(map[string]EdgeData),
		transCount: make(map[BondType]map[BondType]int),
		chat:       chat,
		st:         st,
	}
	mg.loadFromDisk()
	return mg
}

// AddStep – single step addition
func (mg *MemoryGraph) AddStep(sessionID, content string, parentID string) (string, error) {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	id := uuid.NewString()

	// Add only the key to the mole_syn library
	if err := mg.g.AddVertex(id); err != nil {
		return "", fmt.Errorf("add vertex failed: %w", err)
	}

	// Store rich node data separately
	node := Node{
		ID:      id,
		Content: content,
		Meta:    map[string]any{"session": sessionID},
	}
	mg.vertexData[id] = node

	if parentID != "" {
		if err := mg.g.AddEdge(parentID, id); err != nil {
			return "", fmt.Errorf("add edge failed: %w", err)
		}
		edgeKey := parentID + "->" + id
		mg.edgeData[edgeKey] = EdgeData{Bond: Explore} // default – can classify later
	}

	mg.lastNode[sessionID] = id
	mg.saveToDiskLocked()
	return id, nil
}

// Batch insert from Mole-Syn topology analysis
func (mg *MemoryGraph) AddStepsFromAnalysis(sessionID string, analysis *TopologyAnalysis) error {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	lastNodeID := mg.lastNode[sessionID]
	stepIDMap := make(map[int]string)

	for _, step := range analysis.Steps {
		id := uuid.NewString()

		// Add key only
		if err := mg.g.AddVertex(id); err != nil {
			return fmt.Errorf("add vertex %s failed: %w", id, err)
		}

		// Store node data
		mg.vertexData[id] = Node{
			ID:      id,
			Content: step.Content,
			Meta:    map[string]any{"analysis_step_id": step.ID},
		}
		stepIDMap[step.ID] = id
	}

	// Link first step to existing session tail
	if lastNodeID != "" && len(analysis.Steps) > 0 {
		firstNewID := stepIDMap[analysis.Steps[0].ID]
		if err := mg.g.AddEdge(lastNodeID, firstNewID); err == nil {
			edgeKey := lastNodeID + "->" + firstNewID
			mg.edgeData[edgeKey] = EdgeData{Bond: Explore}
		}
	}

	// Add edges from analysis bonds
	for _, b := range analysis.Bonds {
		fromID, ok1 := stepIDMap[b.From]
		toID, ok2 := stepIDMap[b.To]
		if !ok1 || !ok2 {
			continue
		}

		if err := mg.g.AddEdge(fromID, toID); err != nil {
			continue
		}

		edgeKey := fromID + "->" + toID
		bondType := BondType(strings.ToLower(b.Type))
		mg.edgeData[edgeKey] = EdgeData{Bond: bondType}

		// Update transition stats
		prevBond := mg.getPrevBond(fromID)
		if mg.transCount[prevBond] == nil {
			mg.transCount[prevBond] = make(map[BondType]int)
		}
		mg.transCount[prevBond][bondType]++
	}

	if len(analysis.Steps) > 0 {
		// Update tail to the last step in the slice
		mg.lastNode[sessionID] = stepIDMap[analysis.Steps[len(analysis.Steps)-1].ID]
	}

	mg.saveToDiskLocked()
	return nil
}

// Helper to get node content (example usage)
func (mg *MemoryGraph) GetNodeContent(id string) (string, bool) {
	mg.mu.RLock()
	defer mg.mu.RUnlock()
	node, ok := mg.vertexData[id]
	if !ok {
		return "", false
	}
	return node.Content, true
}

// Helper to get bond type of incoming edge to nodeID
func (mg *MemoryGraph) getPrevBond(nodeID string) BondType {
	preds, err := mg.g.PredecessorMap()
	if err != nil {
		return Explore
	}

	p, ok := preds[nodeID]
	if !ok || len(p) == 0 {
		return Explore
	}

	// In our mostly linear chain, there's usually one predecessor
	for predID := range p {
		edgeKey := predID + "->" + nodeID
		if data, ok := mg.edgeData[edgeKey]; ok {
			return data.Bond
		}
	}

	return Explore
}

// Simple async save
func (mg *MemoryGraph) saveToDisk() {
	mg.mu.RLock()
	defer mg.mu.RUnlock()
	mg.saveToDiskLocked()
}

func (mg *MemoryGraph) saveToDiskLocked() {
	data := struct {
		LastNode   map[string]string             `json:"last_node"`
		VertexData map[string]Node               `json:"vertex_data"`
		EdgeData   map[string]EdgeData           `json:"edge_data"`
		TransCount map[BondType]map[BondType]int `json:"trans_count"`
	}{
		LastNode:   mg.lastNode,
		VertexData: mg.vertexData,
		EdgeData:   mg.edgeData,
		TransCount: mg.transCount,
	}

	if err := mg.st.SaveState("mole_syn_graph", data); err != nil {
		slog.Error("marshal/save mole_syn failed", "error", err)
	}
}

func (mg *MemoryGraph) loadFromDisk() {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	var data struct {
		LastNode   map[string]string             `json:"last_node"`
		VertexData map[string]Node               `json:"vertex_data"`
		EdgeData   map[string]EdgeData           `json:"edge_data"`
		TransCount map[BondType]map[BondType]int `json:"trans_count"`
	}

	if err := mg.st.LoadState("mole_syn_graph", &data); err != nil {
		if !strings.Contains(err.Error(), "no such file or directory") {
			slog.Error("read mole_syn file failed", "error", err)
		}
		return
	}

	if data.LastNode != nil {
		mg.lastNode = data.LastNode
	}
	if data.VertexData != nil {
		mg.vertexData = data.VertexData
	}
	if data.EdgeData != nil {
		mg.edgeData = data.EdgeData
	}
	if data.TransCount != nil {
		mg.transCount = data.TransCount
	}

	// Reconstruct the mole_syn lib structure
	for id := range mg.vertexData {
		_ = mg.g.AddVertex(id)
	}

	for key := range mg.edgeData {
		parts := strings.Split(key, "->")
		if len(parts) == 2 {
			_ = mg.g.AddEdge(parts[0], parts[1])
		}
	}
}
