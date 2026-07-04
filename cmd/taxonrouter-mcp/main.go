package main

import (
	"context"
	"os"

	"github.com/LOUST-PRO/TaxonRouter/internal/mcp"
)

func main() {
	env := mcp.LoadEnv()
	ctx := context.Background()
	if err := mcp.Run(ctx, env); err != nil {
		os.Stderr.WriteString("taxonrouter-mcp: " + err.Error() + "\n")
		os.Exit(1)
	}
}
