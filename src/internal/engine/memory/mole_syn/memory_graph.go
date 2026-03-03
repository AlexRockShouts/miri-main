package mole_syn

import (
	"context"
	"fmt"
	"log/slog"
	"miri-main/src/internal/storage"
	"strings"
	"sync"
	"time"

	"slices"

	"github.com/cloudwego/eino/components/model"
	"github.com/dominikbraun/graph"
	"github.com/google/uuid"
)

type SearchResult struct {
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata"`
}

// We need an interface to avoid circular dependency if we use memory.MemorySystem
type MemorySystem interface {
	Add(ctx context.Context, content string, metadata map[string]string) error
	ListAll(ctx context.Context) ([]SearchResult, error)
	Delete(ctx context.Context, id string) error
}

type BondType string

const (
	Deep    BondType = "D"
	Reflect BondType = "R"
	Explore BondType = "E"
)

func mapBondType(s string) BondType {
	switch strings.ToUpper(s) {
	case "D", "DEEP":
		return Deep
	case "R", "REFLECT":
		return Reflect
	case "E", "EXPLORE":
		return Explore
	default:
		return Explore
	}
}

type Node struct {
	ID      string         `json:"id"`
	Content string         `json:"content"`
	Meta    map[string]any `json:"meta,omitempty"`
}

type EdgeData struct {
	Bond BondType `json:"bond"`
}

type MemoryGraph struct {
	g                  graph.Graph[string, string]   // we don't use the V type meaningfully
	vertexData         map[string]Node               // ← IMPORTANT: store your Node here
	lastNode           map[string]string             // sessionID → last node ID
	edgeData           map[string]EdgeData           // key = "from->to"
	transCount         map[BondType]map[BondType]int // from-bond → to-bond → count
	maxNodesPerSession int                           // 0 = unlimited
	mu                 sync.RWMutex
	chat               model.BaseChatModel
	st                 *storage.Storage
	ms                 MemorySystem
}

func New(chat model.BaseChatModel, st *storage.Storage, ms MemorySystem, maxNodesPerSession int) *MemoryGraph {
	g := graph.New(graph.StringHash, graph.Directed())
	mg := &MemoryGraph{
		g:                  g,
		vertexData:         make(map[string]Node),
		lastNode:           make(map[string]string),
		edgeData:           make(map[string]EdgeData),
		transCount:         make(map[BondType]map[BondType]int),
		maxNodesPerSession: maxNodesPerSession,
		chat:               chat,
		st:                 st,
		ms:                 ms,
	}
	mg.loadFromMemorySystem()
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

	bond := Explore
	metadata := map[string]string{
		"session":   sessionID,
		"parent_id": parentID,
		"bond":      string(bond),
		"timestamp": time.Now().Format(time.RFC3339Nano),
		"id":        id,
	}

	// Store rich node data separately
	node := Node{
		ID:      id,
		Content: content,
		Meta:    map[string]any{"session": sessionID, "parent_id": parentID, "bond": string(bond), "timestamp": metadata["timestamp"]},
	}
	mg.vertexData[id] = node

	if parentID != "" {
		if err := mg.g.AddEdge(parentID, id); err != nil {
			return "", fmt.Errorf("add edge failed: %w", err)
		}
		edgeKey := parentID + "->" + id
		mg.edgeData[edgeKey] = EdgeData{Bond: bond}
	}

	mg.lastNode[sessionID] = id
	mg.pruneSessionLocked(sessionID)

	// Save to vector memory
	if mg.ms != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = mg.ms.Add(ctx, content, metadata)
	}

	return id, nil
}

// Batch insert from Mole-Syn topology analysis
func (mg *MemoryGraph) AddStepsFromAnalysis(sessionID string, analysis *TopologyAnalysis) error {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	lastNodeID := mg.lastNode[sessionID]
	stepIDMap := make(map[int]string)
	baseTime := time.Now()

	for i, step := range analysis.Steps {
		id := uuid.NewString()
		// Each step gets a unique nanosecond-precision timestamp so that
		// loadFromMemorySystem can deterministically reconstruct lastNode.
		ts := baseTime.Add(time.Duration(i) * time.Nanosecond).Format(time.RFC3339Nano)

		// Add key only
		if err := mg.g.AddVertex(id); err != nil {
			return fmt.Errorf("add vertex %s failed: %w", id, err)
		}

		// Store node data
		mg.vertexData[id] = Node{
			ID:      id,
			Content: step.Content,
			Meta:    map[string]any{"analysis_step_id": step.ID, "session": sessionID, "timestamp": ts},
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
	// Build predecessor map once before the loop — O(V+E) total instead of O(N×(V+E)).
	bondsPM, _ := mg.g.PredecessorMap()
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
		bondType := mapBondType(b.Type)
		mg.edgeData[edgeKey] = EdgeData{Bond: bondType}

		// Update node metadata with bond and parent
		node := mg.vertexData[toID]
		node.Meta["bond"] = string(bondType)
		node.Meta["parent_id"] = fromID
		mg.vertexData[toID] = node

		// Update transition stats using the pre-built predecessor map.
		prevBond := mg.prevBondFromMap(bondsPM, fromID)
		if mg.transCount[prevBond] == nil {
			mg.transCount[prevBond] = make(map[BondType]int)
		}
		mg.transCount[prevBond][bondType]++
	}

	if len(analysis.Steps) > 0 {
		// Update tail to the last step in the slice
		mg.lastNode[sessionID] = stepIDMap[analysis.Steps[len(analysis.Steps)-1].ID]
		mg.pruneSessionLocked(sessionID)
	}

	// Save to vector memory
	if mg.ms != nil {
		newIDs := make(map[string]bool, len(stepIDMap))
		for _, sid := range stepIDMap {
			newIDs[sid] = true
		}
		for id, node := range mg.vertexData {
			// Only save nodes from current analysis
			if s, ok := node.Meta["session"].(string); !ok || s != sessionID {
				continue
			}
			// Only save if it's one of the newly added steps
			if !newIDs[id] {
				continue
			}

			nodeTS, _ := node.Meta["timestamp"].(string)
			metadata := map[string]string{
				"session":   sessionID,
				"timestamp": nodeTS,
				"id":        id,
			}
			if pid, ok := node.Meta["parent_id"].(string); ok {
				metadata["parent_id"] = pid
			}
			if b, ok := node.Meta["bond"].(string); ok {
				metadata["bond"] = b
			}

			// Capture values for closure
			cid := id
			ccontent := node.Content
			cmeta := metadata
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				// We use Update to ensure we can set the ID if the MemorySystem supports it
				// But our Add uses chromem-go AddDocument which takes ID.
				// Our interface Add doesn't take ID, it generates one if not present in metadata.
				// Wait, I should probably use a specialized Add that takes ID or put ID in metadata.
				cmeta["id"] = cid
				_ = mg.ms.Add(ctx, ccontent, cmeta)
			}()
		}
	}

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

// GetStrongPath retrieves a path of the most significant reasoning nodes for a session.
// It uses BFS/DFS starting from the root(s) of the session, preferring D (Deep Reasoning) bonds,
// then R (Self-Reflection), and limiting E (Self-Exploration) branches.
// Add this method to MemoryGraph struct
func (mg *MemoryGraph) GetStrongPath(sessionID string, maxDepth int) []string {
	mg.mu.RLock()
	defer mg.mu.RUnlock()

	lastID, exists := mg.lastNode[sessionID]
	if !exists {
		return nil
	}

	path := []string{}
	current := lastID
	visited := make(map[string]bool)
	depth := 0

	pm, _ := mg.g.PredecessorMap()

	for current != "" && depth < maxDepth && !visited[current] {
		visited[current] = true
		path = append([]string{current}, path...) // prepend for chronological order

		preds := pm[current]
		if len(preds) == 0 {
			break
		}

		// Prefer Deep bond first, then Reflect, then Explore
		var bestPred string
		for pred := range preds {
			edgeKey := pred + "->" + current
			data, ok := mg.edgeData[edgeKey]
			if !ok {
				continue
			}

			if bestPred == "" {
				bestPred = pred
				continue
			}

			bestBond := mg.edgeData[bestPred+"->"+current].Bond
			if data.Bond == Deep && bestBond != Deep {
				bestPred = pred
			} else if data.Bond == Reflect && (bestBond != Deep && bestBond != Reflect) {
				bestPred = pred
			}
		}

		if bestPred == "" {
			for p := range preds {
				bestPred = p
				break
			}
		}
		current = bestPred
		depth++
	}

	return path
}

// Helper: build formatted context from path
func (mg *MemoryGraph) BuildGraphContext(path []string) string {
	var sb strings.Builder
	sb.WriteString("Long-term reasoning backbone (stable Mole-Syn paths):\n")

	for i, id := range path {
		if content, ok := mg.GetNodeContent(id); ok {
			bondLabel := ""
			if i < len(path)-1 {
				nextID := path[i+1]
				edgeKey := id + "->" + nextID
				if data, ok := mg.edgeData[edgeKey]; ok {
					bondLabel = fmt.Sprintf(" [%s]", strings.ToUpper(string(data.Bond)))
				}
			}
			sb.WriteString(fmt.Sprintf("%s%s\n", content, bondLabel))
		}
	}
	return sb.String()
}

// Helper to get bond type of incoming edge to nodeID
// TopologyData is a serializable representation of the memory graph
type TopologyData struct {
	Nodes []Node      `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type GraphEdge struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Bond BondType `json:"bond"`
}

// GetTopology returns a serializable representation of the graph
func (mg *MemoryGraph) GetTopology(sessionID string) (*TopologyData, error) {
	mg.mu.RLock()
	defer mg.mu.RUnlock()

	res := &TopologyData{
		Nodes: make([]Node, 0),
		Edges: make([]GraphEdge, 0),
	}

	// Collect nodes
	for _, node := range mg.vertexData {
		if sessionID != "" {
			sess, ok := node.Meta["session"].(string)
			if !ok || sess != sessionID {
				continue
			}
		}
		res.Nodes = append(res.Nodes, node)
	}

	// Collect edges
	for edgeKey, data := range mg.edgeData {
		parts := strings.Split(edgeKey, "->")
		if len(parts) != 2 {
			continue
		}
		from, to := parts[0], parts[1]

		// If filtering by session, ensure both nodes belong to that session
		if sessionID != "" {
			nodeFrom, okFrom := mg.vertexData[from]
			nodeTo, okTo := mg.vertexData[to]
			if !okFrom || !okTo {
				continue
			}
			sessFrom, okSessFrom := nodeFrom.Meta["session"].(string)
			sessTo, okSessTo := nodeTo.Meta["session"].(string)
			if !okSessFrom || sessFrom != sessionID || !okSessTo || sessTo != sessionID {
				continue
			}
		}

		res.Edges = append(res.Edges, GraphEdge{
			From: from,
			To:   to,
			Bond: data.Bond,
		})
	}

	return res, nil
}

// prevBondFromMap returns the bond type of the incoming edge to nodeID using a
// pre-built predecessor map. Callers must build the map once before any loop.
func (mg *MemoryGraph) prevBondFromMap(pm map[string]map[string]graph.Edge[string], nodeID string) BondType {
	p, ok := pm[nodeID]
	if !ok || len(p) == 0 {
		return Explore
	}
	for predID := range p {
		edgeKey := predID + "->" + nodeID
		if data, ok := mg.edgeData[edgeKey]; ok {
			return data.Bond
		}
	}
	return Explore
}

func (mg *MemoryGraph) loadFromMemorySystem() {
	if mg.ms == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, err := mg.ms.ListAll(ctx)
	if err != nil {
		slog.Error("failed to load mole_syn steps from vector memory", "error", err)
		return
	}

	mg.mu.Lock()
	defer mg.mu.Unlock()

	// Temp map to store nodes to rebuild lastNode per session
	sessionLastTS := make(map[string]time.Time)

	for _, res := range results {
		id := res.Metadata["id"]
		if id == "" {
			continue
		}

		// Reconstruct Node
		node := Node{
			ID:      id,
			Content: res.Content,
			Meta:    make(map[string]any),
		}
		for k, v := range res.Metadata {
			node.Meta[k] = v
		}
		mg.vertexData[id] = node

		// Add to graph
		_ = mg.g.AddVertex(id)

		// Reconstruct lastNode
		if session, ok := res.Metadata["session"]; ok {
			if tsStr, ok := res.Metadata["timestamp"]; ok {
				ts, _ := time.Parse(time.RFC3339Nano, tsStr)
				if ts.After(sessionLastTS[session]) || sessionLastTS[session].IsZero() {
					sessionLastTS[session] = ts
					mg.lastNode[session] = id
				}
			} else {
				// Fallback if no timestamp, use the last one encountered
				mg.lastNode[session] = id
			}
		}
	}

	// Second pass: Reconstruct edges.
	// Build predecessor map once for the entire pass — O(V+E) total.
	loadPM, _ := mg.g.PredecessorMap()
	for id, node := range mg.vertexData {
		var parentID string
		if pid, ok := node.Meta["parent_id"].(string); ok && pid != "" {
			parentID = pid
		} else if pid, ok := node.Meta["parent_id"]; ok {
			// In case it's not a string in the map
			parentID = fmt.Sprintf("%v", pid)
		}

		if parentID != "" {
			_ = mg.g.AddEdge(parentID, id)
			bond := Explore
			if b, ok := node.Meta["bond"].(string); ok {
				bond = mapBondType(b)
			} else if b, ok := node.Meta["bond"]; ok {
				bond = mapBondType(fmt.Sprintf("%v", b))
			}
			edgeKey := parentID + "->" + id
			mg.edgeData[edgeKey] = EdgeData{Bond: bond}

			// Update transition stats using the pre-built predecessor map.
			prevBond := mg.prevBondFromMap(loadPM, parentID)
			if mg.transCount[prevBond] == nil {
				mg.transCount[prevBond] = make(map[BondType]int)
			}
			mg.transCount[prevBond][bond]++
		}
	}

	slog.Info("Mole-Syn graph loaded from vector memory", "nodes", len(mg.vertexData))
}

// pruneSessionLocked removes the oldest nodes for a session when the node count exceeds
// maxNodesPerSession. It must be called with mg.mu held for writing.
func (mg *MemoryGraph) pruneSessionLocked(sessionID string) {
	if mg.maxNodesPerSession <= 0 {
		return
	}

	// Collect all nodes belonging to this session.
	type nodeTS struct {
		id string
		ts time.Time
	}
	var nodes []nodeTS
	for id, node := range mg.vertexData {
		if s, _ := node.Meta["session"].(string); s != sessionID {
			continue
		}
		var ts time.Time
		if tsStr, _ := node.Meta["timestamp"].(string); tsStr != "" {
			ts, _ = time.Parse(time.RFC3339Nano, tsStr)
		}
		nodes = append(nodes, nodeTS{id: id, ts: ts})
	}

	excess := len(nodes) - mg.maxNodesPerSession
	if excess <= 0 {
		return
	}

	// Sort ascending by timestamp so the oldest are first.
	slices.SortFunc(nodes, func(a, b nodeTS) int {
		return a.ts.Compare(b.ts)
	})

	for _, n := range nodes[:excess] {
		// Remove edges referencing this node.
		for key := range mg.edgeData {
			if strings.HasPrefix(key, n.id+"->") || strings.HasSuffix(key, "->"+n.id) {
				delete(mg.edgeData, key)
			}
		}
		_ = mg.g.RemoveVertex(n.id)
		delete(mg.vertexData, n.id)
		// Remove from vector memory asynchronously.
		if mg.ms != nil {
			rid := n.id
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = mg.ms.Delete(ctx, rid)
			}()
		}
	}
	slog.Debug("mole_syn: pruned old nodes", "session", sessionID, "removed", excess)
}

// Simple async save (obsolete, keeping for compatibility if needed, but does nothing now)
func (mg *MemoryGraph) saveToDisk() {}

func (mg *MemoryGraph) saveToDiskLocked() {}

func (mg *MemoryGraph) loadFromDisk() {}
