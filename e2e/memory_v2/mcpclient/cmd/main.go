// Command memory-mcp calls the built-in HTTP MCP using a locally seeded profile.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Tencent/WeKnora/e2e/memory_v2/mcpclient"
)

func main() {
	seed := flag.String("seed", "e2e/memory_v2/seed-output.json", "local seed output; contains test credentials")
	profile := flag.String("profile", "acme-rw", "seeded endpoint profile")
	tool := flag.String("tool", "", "tool name; empty lists tools")
	args := flag.String("args", "{}", "JSON tool arguments")
	config := flag.Bool("mcp-config", false, "print Claude Code MCP configuration; output contains a secret")
	flag.Parse()
	b, err := os.ReadFile(*seed)
	if err != nil {
		log.Fatal(err)
	}
	var world struct {
		BaseURL string                                  `json:"base_url"`
		MCP     map[string]struct{ Path, Token string } `json:"mcp_endpoints"`
	}
	if err = json.Unmarshal(b, &world); err != nil {
		log.Fatal(err)
	}
	ep, ok := world.MCP[*profile]
	if !ok {
		log.Fatalf("unknown endpoint profile %q", *profile)
	}
	if *config {
		emit(map[string]any{"mcpServers": map[string]any{"memory-e2e": map[string]any{
			"type": "http", "url": world.BaseURL + ep.Path,
			"headers": map[string]string{"Authorization": "Bearer " + ep.Token},
		}}})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	session, err := mcpclient.Connect(ctx, world.BaseURL, ep.Path, ep.Token)
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()
	if *tool == "" {
		result, err := session.Tools(ctx)
		if err != nil {
			log.Fatal(err)
		}
		emit(result)
		return
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(*args), &input); err != nil {
		log.Fatal(err)
	}
	var result any
	if err := session.Call(ctx, *tool, input, &result); err != nil {
		log.Fatal(err)
	}
	emit(result)
}

func emit(value any) {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(b))
}
