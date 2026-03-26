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

// BondWeight returns the default numeric weight for a bond type.
// Deep reasoning bonds carry the most weight, followed by self-reflection,
// then exploratory steps.
func BondWeight(b BondType) float64 {
	switch b {
	case Deep:
		return 1.0
	case Reflect:
		return 0.7
	case Explore:
		return 0.3
	default:
		return 0.3
	}
}

type Node struct {
	ID         string         `json:"id"`
	Content    string         `json:"content"`
	Meta       map[string]any `json:"meta,omitempty"`
	Importance float64        `json:"importance"`
}

type EdgeData struct {
	Bond   BondType `json:"bond"`
	Weight float64  `json:"weight"`
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
	mg.loadFromMemorySystem(context.Background())
	return mg
}

// AddStep – single step addition
func (mg *MemoryGraph) AddStep(ctx context.Context, sessionID, content string, parentID string) (string, error) {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	id := uuid.NewString()

	// Add only the key to the mole_syn library
	if err := mg.g.AddVertex(id); err != nil {
		return "", fmt.Errorf("add vertex failed: %w", err)
	}

	bond := Explore
	importance := 0.5 // default importance for single-step additions
	metadata := map[string]string{
		"session":    sessionID,
		"parent_id":  parentID,
		"bond":       string(bond),
		"weight":     fmt.Sprintf("%.2f", BondWeight(bond)),
		"importance": fmt.Sprintf("%.2f", importance),
		"timestamp":  time.Now().Format(time.RFC3339Nano),
		"id":         id,
	}

	// Store rich node data separately
	node := Node{
		ID:         id,
		Content:    content,
		Meta:       map[string]any{"session": sessionID, "parent_id": parentID, "bond": string(bond), "timestamp": metadata["timestamp"]},
		Importance: importance,
	}
	mg.vertexData[id] = node

	if parentID != "" {
		if err := mg.g.AddEdge(parentID, id); err != nil {
			return "", fmt.Errorf("add edge failed: %w", err)
		}
		edgeKey := parentID + "->" + id
		mg.edgeData[edgeKey] = EdgeData{Bond: bond, Weight: BondWeight(bond)}
	}

	mg.lastNode[sessionID] = id
	mg.pruneSessionLocked(ctx, sessionID)

	// Save to vector memory
	if mg.ms != nil {
		saveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = mg.ms.Add(saveCtx, content, metadata)
	}

	return id, nil
}

// Batch insert from Mole-Syn topology analysis
func (mg *MemoryGraph) AddStepsFromAnalysis(ctx context.Context, sessionID string, analysis *TopologyAnalysis) error {
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

		// Store node data with default importance for analysis-derived steps
		mg.vertexData[id] = Node{
			ID:         id,
			Content:    step.Content,
			Meta:       map[string]any{"analysis_step_id": step.ID, "session": sessionID, "timestamp": ts},
			Importance: 0.6,
		}
		stepIDMap[step.ID] = id
	}

	// Link first step to existing session tail
	if lastNodeID != "" && len(analysis.Steps) > 0 {
		firstNewID := stepIDMap[analysis.Steps[0].ID]
		if err := mg.g.AddEdge(lastNodeID, firstNewID); err == nil {
			edgeKey := lastNodeID + "->" + firstNewID
			mg.edgeData[edgeKey] = EdgeData{Bond: Explore, Weight: BondWeight(Explore)}
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
		mg.edgeData[edgeKey] = EdgeData{Bond: bondType, Weight: BondWeight(bondType)}

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
		mg.pruneSessionLocked(ctx, sessionID)
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
				"session":    sessionID,
				"timestamp":  nodeTS,
				"id":         id,
				"importance": fmt.Sprintf("%.2f", node.Importance),
			}
			if pid, ok := node.Meta["parent_id"].(string); ok {
				metadata["parent_id"] = pid
			}
			if b, ok := node.Meta["bond"].(string); ok {
				metadata["bond"] = b
				metadata["weight"] = fmt.Sprintf("%.2f", BondWeight(mapBondType(b)))
			}

			// Capture values for closure
			cid := id
			ccontent := node.Content
			cmeta := metadata
			go func() {
				saveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				cmeta["id"] = cid
				_ = mg.ms.Add(saveCtx, ccontent, cmeta)
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

		// Select predecessor with highest edge weight (combines bond type + importance)
		var bestPred string
		var bestScore float64
		for pred := range preds {
			edgeKey := pred + "->" + current
			data, ok := mg.edgeData[edgeKey]
			if !ok {
				if bestPred == "" {
					bestPred = pred
				}
				continue
			}
			// Score = edge weight + predecessor node importance
			score := data.Weight
			if predNode, ok := mg.vertexData[pred]; ok {
				score += predNode.Importance * 0.4
			}
			if bestPred == "" || score > bestScore {
				bestPred = pred
				bestScore = score
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
	From   string   `json:"from"`
	To     string   `json:"to"`
	Bond   BondType `json:"bond"`
	Weight float64  `json:"weight"`
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
			From:   from,
			To:     to,
			Bond:   data.Bond,
			Weight: data.Weight,
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

func (mg *MemoryGraph) loadFromMemorySystem(ctx context.Context) {
	if mg.ms == nil {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
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

		// Reconstruct Node with importance
		var importance float64
		if impStr, ok := res.Metadata["importance"]; ok {
			fmt.Sscanf(impStr, "%f", &importance)
		}
		node := Node{
			ID:         id,
			Content:    res.Content,
			Meta:       make(map[string]any),
			Importance: importance,
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
			mg.edgeData[edgeKey] = EdgeData{Bond: bond, Weight: BondWeight(bond)}

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
func (mg *MemoryGraph) pruneSessionLocked(ctx context.Context, sessionID string) {
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
	if excess > 0 {
		// Cycle-aware pruning
		sessionNodes := make(map[string]struct{})
		for _, nt := range nodes {
			sessionNodes[nt.id] = struct{}{}
		}
		sg := graph.New(graph.StringHash, graph.Directed())
		for id := range sessionNodes {
			_ = sg.AddVertex(id)
		}
		for edgeKey := range mg.edgeData {
			parts := strings.Split(edgeKey, "->")
			if len(parts) == 2 {
				from, to := parts[0], parts[1]
				if _, ok1 := sessionNodes[from]; ok1 {
					if _, ok2 := sessionNodes[to]; ok2 {
						_ = sg.AddEdge(from, to)
					}
				}
			}
		}
		_, topoErr := graph.TopologicalSort(sg)
		if topoErr != nil {
			extra := len(nodes) / 10
			if extra < 5 {
				extra = 5
			}
			excess += extra
			slog.Debug("Mole-Syn: cycles in session graph, pruning extra nodes", "session", sessionID, "extra", extra)
		}
	}

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
				delCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				_ = mg.ms.Delete(delCtx, rid)
			}()
		}
	}
	slog.Debug("mole_syn: pruned old nodes", "session", sessionID, "removed", excess)
}
