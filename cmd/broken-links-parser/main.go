package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/n-boshnakov/broken-links-parser/internal/extractor"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var (
	dirs    []string
	rootDir string
)

var rootCmd = &cobra.Command{
	Use:   "broken-links-parser",
	Short: "Find and fix broken links in Markdown and HTML documentation repositories",
}

var extractCmd = &cobra.Command{
	Use:   "extract",
	Short: "Extract all links from a repository and print a summary",
	RunE: func(cmd *cobra.Command, _ []string) error {
		links, err := extractor.Extract(rootDir, dirs)
		if err != nil {
			return err
		}
		fmt.Printf("Found %d links in %s\n", len(links), rootDir)
		if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
			for _, l := range links {
				rel, _ := strings.CutPrefix(l.SourceFile, rootDir+"/")
				fmt.Printf("  [%s] %s  (%s)\n", l.Type, l.URL, rel)
			}
		}
		return nil
	},
}

func init() {
	extractCmd.Flags().StringVar(&rootDir, "root", ".", "Repository root directory")
	extractCmd.Flags().StringSliceVar(&dirs, "dirs", nil, "Restrict scan to comma-separated subdirectories")
	extractCmd.Flags().Bool("verbose", false, "Print each link")
	rootCmd.AddCommand(extractCmd)
}
