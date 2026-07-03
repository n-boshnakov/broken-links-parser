package validator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func TestValidate_DocforgeStrict(t *testing.T) {
	dir := t.TempDir()
	// Create a source file and a target file in a "clone".
	sourceFile := filepath.Join(dir, "source.md")
	targetFile := filepath.Join(dir, "target.md")
	_ = os.WriteFile(sourceFile, []byte(""), 0o644)
	_ = os.WriteFile(targetFile, []byte(""), 0o644)

	link := types.Link{
		URL:        "target.md",
		Type:       types.LinkTypeRelative,
		SourceFile: sourceFile,
		SourceRepo: dir,
	}

	t.Run("strict=false — valid link not annotated", func(t *testing.T) {
		results := Validate([]types.Link{link}, ValidateOptions{})
		if !results[0].Valid {
			t.Error("expected valid")
		}
		if results[0].NotAssembled {
			t.Error("NotAssembled should be false when strict mode is off")
		}
	})

	t.Run("strict=true, target in SourceMap — not annotated", func(t *testing.T) {
		sm := NewSourceMapper([]string{targetFile})
		results := Validate([]types.Link{link}, ValidateOptions{
			DocforgeStrict: true,
			SourceMap:      sm,
		})
		if !results[0].Valid {
			t.Error("expected valid")
		}
		if results[0].NotAssembled {
			t.Error("NotAssembled should be false when target is in SourceMap")
		}
	})

	t.Run("strict=true, target NOT in SourceMap — annotated", func(t *testing.T) {
		sm := NewSourceMapper([]string{}) // empty map
		results := Validate([]types.Link{link}, ValidateOptions{
			DocforgeStrict: true,
			SourceMap:      sm,
		})
		if !results[0].Valid {
			t.Error("expected valid (file exists on disk)")
		}
		if !results[0].NotAssembled {
			t.Error("NotAssembled should be true when target not in SourceMap")
		}
	})
}
