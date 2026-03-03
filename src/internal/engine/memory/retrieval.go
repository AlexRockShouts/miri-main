package memory

import (
	"context"
	"fmt"
	"miri-main/src/internal/engine/memory/mole_syn"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
)

func (b *Brain) Retrieve(ctx context.Context, sessionID, query string) (string, error) {
	docs, err := b.RetrieveDocuments(ctx, sessionID, query)
	if err != nil {
		return "", err
	}
	if len(docs) == 0 {
		return "", nil
	}

	var sb strings.Builder
	for _, doc := range docs {
		sb.WriteString(doc.Content)
		if !strings.HasSuffix(doc.Content, "\n") {
			sb.WriteString("\n")
		}
	}
	return sb.String(), nil
}

func (b *Brain) RetrieveDocuments(ctx context.Context, sessionID, query string) ([]*schema.Document, error) {
	if b.factMemory == nil || b.summaryMemory == nil {
		return nil, nil
	}

	b.mu.RLock()
	count := b.interactionCount
	deepRatio := b.lastDeepBondRatio
	usage := b.lastContextUsage
	window := b.contextWindow
	b.mu.RUnlock()

	// Also trigger if context usage is already known to be high
	if window > 0 && float64(usage)/float64(window) >= 0.6 {
		go b.TriggerMaintenance(TriggerContextUsage)
	}

	var finalDocs []*schema.Document

	graphSteps := b.retrieval.GraphSteps
	if graphSteps <= 0 {
		graphSteps = 5
	}
	factsTopK := b.retrieval.FactsTopK
	if factsTopK <= 0 {
		factsTopK = 5
	}
	summariesTopK := b.retrieval.SummariesTopK
	if summariesTopK <= 0 {
		summariesTopK = 3
	}
	// 1. Graph Recall (Structural Priority)
	if b.Graph != nil && sessionID != "" {
		path := b.Graph.GetStrongPath(sessionID, graphSteps)
		if len(path) > 0 {
			graphCtx := b.Graph.BuildGraphContext(path)
			finalDocs = append(finalDocs, &schema.Document{
				Content: "### Reasoning Backbone (Mole-Syn) ###\n" + graphCtx + "\n",
				MetaData: map[string]any{
					"type": "graph_priority",
				},
			})
		}
	}

	// 2. Vector Recall (top facts + summaries)
	facts, _ := b.factMemory.Search(ctx, query, factsTopK, nil)
	summaries, _ := b.summaryMemory.Search(ctx, query, summariesTopK, nil)

	results := append(facts, summaries...)
	if len(results) == 0 && len(finalDocs) == 0 {
		return nil, nil
	}

	// 3. Hybrid ranking (weighted score)
	// Primary signal: deep_bond_uses — how often this fact fed into core (Deep-bond) reasoning.
	// Each use in a Deep-bond session reduces effective distance by 5%, capped at 50%.
	// Tie-breaker: topology_score (birth-quality of the session that created the fact).
	sort.SliceStable(results, func(i, j int) bool {
		getScore := func(r SearchResult) float32 {
			dbu, _ := strconv.Atoi(r.Metadata["deep_bond_uses"])
			boost := 1.0 - min(float32(dbu)*0.05, 0.5)
			return r.Distance * boost
		}

		scoreI := getScore(results[i])
		scoreJ := getScore(results[j])

		if scoreI != scoreJ {
			return scoreI < scoreJ
		}

		// Tie-breaker: higher birth-quality topology score first
		tsI, _ := strconv.Atoi(results[i].Metadata["topology_score"])
		tsJ, _ := strconv.Atoi(results[j].Metadata["topology_score"])
		return tsI > tsJ
	})

	// Update access metadata for retrieved results
	for i := range results {
		r := &results[i]
		id := r.Metadata["id"]
		if id == "" {
			continue
		}

		// Increment access count and interaction metadata.
		// topology_score is intentionally NOT overwritten here — it is a stable birth-quality
		// signal set when the fact was created and should not drift with the current session.
		accStr := r.Metadata["access_count"]
		acc, _ := strconv.Atoi(accStr)
		acc++
		r.Metadata["access_count"] = strconv.Itoa(acc)
		r.Metadata["last_accessed"] = time.Now().Format(time.RFC3339)
		r.Metadata["interaction_count"] = strconv.Itoa(count)

		// Increment deep_bond_uses when the current session has a strong Deep-bond ratio.
		// This is the per-fact importance signal derived from mole_syn topology.
		if deepRatio >= 0.5 {
			dbu, _ := strconv.Atoi(r.Metadata["deep_bond_uses"])
			r.Metadata["deep_bond_uses"] = strconv.Itoa(dbu + 1)
		}

		// Update in the appropriate memory system
		if r.Metadata["type"] == "fact" {
			_ = b.factMemory.Update(ctx, id, r.Content, r.Metadata)
		} else {
			_ = b.summaryMemory.Update(ctx, id, r.Content, r.Metadata)
		}
	}

	if len(results) > 0 {
		var sb strings.Builder
		sb.WriteString("### Retrieved Relevant Memories ###\n")
		for _, r := range results {
			// Add prefix based on type if available
			prefix := ""
			if t, ok := r.Metadata["type"]; ok {
				prefix = fmt.Sprintf("[%s] ", strings.ToUpper(t))
			}
			sb.WriteString(fmt.Sprintf("- %s%s\n", prefix, r.Content))
		}
		finalDocs = append(finalDocs, &schema.Document{
			Content: sb.String(),
			MetaData: map[string]any{
				"type": "vector_memories",
			},
		})
	}

	return finalDocs, nil
}

func (b *Brain) GetFacts(ctx context.Context) ([]SearchResult, error) {
	if b.factMemory == nil {
		return nil, nil
	}
	return b.factMemory.ListAll(ctx)
}

func (b *Brain) GetSummaries(ctx context.Context) ([]SearchResult, error) {
	if b.summaryMemory == nil {
		return nil, nil
	}
	return b.summaryMemory.ListAll(ctx)
}

func (b *Brain) GetTopology(ctx context.Context, sessionID string) (*mole_syn.TopologyData, error) {
	if b.Graph == nil {
		return nil, nil
	}
	return b.Graph.GetTopology(sessionID)
}
