package md

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRepoDocuments parses the Markdown shipped in the repository (the
// design document and the sample vault, when present) and checks the
// structural invariants on each.
func TestRepoDocuments(t *testing.T) {
	files := []string{"../../DESIGN.md", "../../README.md"}
	filepath.Walk("../../examples", func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(p) == ".md" {
			files = append(files, p)
		}
		return nil
	})
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		checkInvariants(t, string(b), Parse(string(b)))
	}
}
