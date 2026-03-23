package topology

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Node struct {
	Name string `json:"name"`
	File string `json:"file"`
}

type Graph struct {
	Nodes map[string]Node     `json:"nodes"`
	Adj   map[string][]string `json:"adj,omitempty"`
	Indeg map[string]int      `json:"-"`
}

type Metrics struct {
	N           int      `json:"nodes"`
	E           int      `json:"edges"`
	Components  int      `json:"components"`
	Cyclomatic  float64  `json:"cyclomatic"`
	AvgValency  float64  `json:"avg_valency_out"`
	MaxValency  int      `json:"max_valency_out"`
	AvgIndeg    float64  `json:"avg_valency_in"`
	MaxIndeg    int      `json:"max_valency_in"`
	Diameter    int      `json:"diameter"`
	HasCycle    bool     `json:"has_cycle"`
	HighValency []string `json:"high_valency_nodes"`
}

func ParseDir(dir string) (*Graph, error) {
	g := &Graph{
		Nodes: make(map[string]Node),
		Adj:   make(map[string][]string),
		Indeg: make(map[string]int),
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		file, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		funcCalls := make(map[string][]string)
		ast.Inspect(file, func(n ast.Node) bool {
			if decl, ok := n.(*ast.FuncDecl); ok {
				name := decl.Name.Name
				if ast.IsExported(name) || strings.HasPrefix(name, "New") || strings.Contains(name, "Tool") || strings.Contains(name, "Info") || strings.Contains(name, "InvokableRun") {
					g.Nodes[name] = Node{Name: name, File: filepath.Base(p)}
					ast.Inspect(decl.Body, func(cn ast.Node) bool {
						if call, ok := cn.(*ast.CallExpr); ok {
							if id, ok := call.Fun.(*ast.Ident); ok && ast.IsExported(id.Name) {
								funcCalls[name] = append(funcCalls[name], id.Name)
							} else if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
								if pid, ok := sel.X.(*ast.Ident); ok {
									callee := pid.Name + "." + sel.Sel.Name
									if ast.IsExported(sel.Sel.Name) {
										funcCalls[name] = append(funcCalls[name], callee)
									}
								}
							}
						}
						return true
					})
				}
			}
			return true
		})
		for caller, callees := range funcCalls {
			g.Adj[caller] = unique(g.Adj[caller], callees)
			for _, callee := range g.Adj[caller] {
				g.Indeg[callee]++
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return g, nil
}

func unique(existing []string, new []string) []string {
	m := make(map[string]bool)
	for _, v := range existing {
		m[v] = true
	}
	for _, v := range new {
		m[v] = true
	}
	res := make([]string, 0, len(m))
	for k := range m {
		res = append(res, k)
	}
	sort.Strings(res)
	return res
}

func (g *Graph) numComponents() int {
	visited := make(map[string]bool)
	count := 0
	for node := range g.Nodes {
		if !visited[node] {
			dfsVisit(g, node, visited)
			count++
		}
	}
	return count
}

func dfsVisit(g *Graph, node string, visited map[string]bool) {
	if visited[node] {
		return
	}
	visited[node] = true
	for _, child := range g.Adj[node] {
		dfsVisit(g, child, visited)
	}
}

func (g *Graph) hasCycle() bool {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	for node := range g.Nodes {
		if !visited[node] {
			if dfsCycle(g, node, visited, recStack) {
				return true
			}
		}
	}
	return false
}

func dfsCycle(g *Graph, node string, visited, recStack map[string]bool) bool {
	visited[node] = true
	recStack[node] = true
	for _, child := range g.Adj[node] {
		if !visited[child] {
			if dfsCycle(g, child, visited, recStack) {
				return true
			}
		} else if recStack[child] {
			return true
		}
	}
	delete(recStack, node)
	return false
}

func (g *Graph) diameter() int {
	maxd := 0
	for start := range g.Nodes {
		dists := bfsDist(g, start)
		for _, d := range dists {
			if d > maxd {
				maxd = d
			}
		}
	}
	return maxd
}

func bfsDist(g *Graph, start string) map[string]int {
	dist := make(map[string]int)
	queue := []string{start}
	dist[start] = 0
	i := 0
	for i < len(queue) {
		cur := queue[i]
		i++
		for _, nei := range g.Adj[cur] {
			if _, has := dist[nei]; !has {
				dist[nei] = dist[cur] + 1
				queue = append(queue, nei)
			}
		}
	}
	return dist
}

func (g *Graph) Metrics() *Metrics {
	n := len(g.Nodes)
	if n == 0 {
		return &Metrics{}
	}
	e := 0
	outdegs := make([]int, 0, n)
	for _, outs := range g.Adj {
		e += len(outs)
		outdegs = append(outdegs, len(outs))
	}
	sort.Ints(outdegs)
	avgout := float64(e) / float64(n)
	maxout := outdegs[len(outdegs)-1]

	sumindeg := 0
	indegs := make([]int, 0, n)
	for _, id := range g.Indeg {
		sumindeg += id
		indegs = append(indegs, id)
	}
	sort.Ints(indegs)
	avgin := float64(sumindeg) / float64(n)
	maxin := indegs[len(indegs)-1]

	c := g.numComponents()
	cyc := float64(e) - float64(n) + float64(c)
	diam := g.diameter()
	cycycle := g.hasCycle()

	// top 5 high outdeg
	type pair struct {
		node string
		deg  int
	}
	pairs := make([]pair, 0, len(g.Adj))
	for node, outs := range g.Adj {
		pairs = append(pairs, pair{node, len(outs)})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].deg > pairs[j].deg })
	high := make([]string, 0, 5)
	for i := 0; i < len(pairs) && i < 5; i++ {
		high = append(high, pairs[i].node)
	}

	return &Metrics{
		N:           n,
		E:           e,
		Components:  c,
		Cyclomatic:  cyc,
		AvgValency:  avgout,
		MaxValency:  maxout,
		AvgIndeg:    avgin,
		MaxIndeg:    maxin,
		Diameter:    diam,
		HasCycle:    cycycle,
		HighValency: high,
	}
}

func Analyze(_ context.Context, dir string) (string, error) {
	g, err := ParseDir(dir)
	if err != nil {
		return "", fmt.Errorf("parse dir %s: %w", dir, err)
	}
	m := g.Metrics()
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
