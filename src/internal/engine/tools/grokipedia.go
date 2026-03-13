package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type GrokipediaInput struct {
	Topic string `json:"topic" jsonschema:"required,description=The topic or article title to look up on Grokipedia"`
}

type GrokipediaOutput struct {
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Citations []string `json:"citations"`
	Error     string   `json:"error,omitempty"`
}

func FetchGrokipediaArticle(ctx context.Context, input *GrokipediaInput) (*GrokipediaOutput, error) {
	normalized := strings.Join(strings.Fields(strings.ToLower(input.Topic)), "_")
	pageURL := fmt.Sprintf("https://grokipedia.com/page/%s", url.QueryEscape(normalized))

	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return &GrokipediaOutput{Error: err.Error()}, err
	}
	req.Header.Set("User-Agent", "Eino-AI-Agent/1.0")

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{DialContext: dialer.DialContext}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &GrokipediaOutput{Error: err.Error()}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &GrokipediaOutput{Error: fmt.Sprintf("HTTP error: %d", resp.StatusCode)}, fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return &GrokipediaOutput{Error: err.Error()}, err
	}

	title := doc.Find("h1.article-title").Text()
	if title == "" {
		title = input.Topic // Fallback
	}

	var content strings.Builder
	doc.Find("div.article-section").Each(func(i int, s *goquery.Selection) {
		content.WriteString(s.Text() + "\n")
	})

	citations := []string{}
	doc.Find("ol.references li").Each(func(i int, s *goquery.Selection) {
		citations = append(citations, s.Text())
	})

	return &GrokipediaOutput{
		Title:     strings.TrimSpace(title),
		Content:   strings.TrimSpace(content.String()),
		Citations: citations,
	}, nil
}

type GrokipediaToolWrapper struct{}

func (g *GrokipediaToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "grokipedia",
		Desc: "Lookup and fetch a fact-checked article from Grokipedia (xAI's encyclopedia) as a reliable source.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"topic": {
				Type:     schema.String,
				Desc:     "The topic or article title to look up on Grokipedia",
				Required: true,
			},
		}),
	}
}

func (g *GrokipediaToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return g.GetInfo(), nil
}

func (g *GrokipediaToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}

	output, err := FetchGrokipediaArticle(ctx, &GrokipediaInput{Topic: args.Topic})
	if err != nil {
		return "", err
	}

	jsonOutput, err := json.Marshal(output)
	if err != nil {
		return "", err
	}
	return string(jsonOutput), nil
}

func CreateGrokipediaTool() tool.InvokableTool {
	return &GrokipediaToolWrapper{}
}
