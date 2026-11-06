package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/tobilg/ai-observer/internal/pricing"
)

const pricingUsage = `Manage the pricing data used to calculate costs

Usage: ai-observer pricing update [options]

Commands:
  update    Fetch current model prices from models.dev (https://models.dev)
            and write claude.json, codex.json, gemini.json and
            github_copilot.json to AI_OBSERVER_PRICING_DIR (or --output).
            AI Observer reads them from AI_OBSERVER_PRICING_DIR at startup,
            over the built-in prices. Models models.dev does not list keep
            their current prices.
`

func cmdPricing(args []string) {
	if err := runPricing(context.Background(), args, os.Stdout, pricing.NewModelsDevFetcher()); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// modelsDevFetcher fetches the models.dev catalog (pricing.ModelsDevFetcher)
type modelsDevFetcher interface {
	Fetch(ctx context.Context) (pricing.ModelsDevCatalog, error)
}

func runPricing(ctx context.Context, args []string, out io.Writer, fetcher modelsDevFetcher) error {
	if len(args) == 0 {
		fmt.Fprint(out, pricingUsage)
		return errors.New("a pricing command is required")
	}
	switch args[0] {
	case "update":
		return runPricingUpdate(ctx, args[1:], out, fetcher, time.Now())
	case "-h", "--help", "help":
		fmt.Fprint(out, pricingUsage)
		return flag.ErrHelp
	default:
		fmt.Fprint(out, pricingUsage)
		return fmt.Errorf("unknown pricing command: %s", args[0])
	}
}

func runPricingUpdate(ctx context.Context, args []string, out io.Writer, fetcher modelsDevFetcher, now time.Time) error {
	fs := flag.NewFlagSet("pricing update", flag.ContinueOnError)
	fs.SetOutput(out)
	var output string
	var opts pricing.UpdateOptions
	fs.StringVar(&output, "output", "", "Directory to write the pricing files to (default: $AI_OBSERVER_PRICING_DIR)")
	fs.BoolVar(&opts.AllowLargeChanges, "allow-large-changes", false, "Apply price changes of more than 10x, which are refused by default")
	fs.Usage = func() {
		fmt.Fprint(out, pricingUsage+"\nOptions:\n")
		printFlags(fs)
	}
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}

	pricingDir := os.Getenv("AI_OBSERVER_PRICING_DIR")
	dir := output
	if dir == "" {
		dir = pricingDir
	}
	if dir == "" {
		return errors.New("--output is required when AI_OBSERVER_PRICING_DIR is not set")
	}

	catalog, err := fetcher.Fetch(ctx)
	if err != nil {
		return err
	}

	// Merge every file before writing any, so a failure leaves all as they were
	type result struct {
		path   string
		update *pricing.PricingUpdate
	}
	var results []result
	for _, target := range pricing.ModelsDevTargets {
		provider, ok := catalog[target.Provider]
		if !ok || len(provider.Models) == 0 {
			return fmt.Errorf("models.dev lists no %s models; keeping the current prices", target.Provider)
		}
		path := filepath.Join(dir, target.File)
		current, err := currentPricingFile(path, target.File)
		if err != nil {
			return err
		}
		update, err := pricing.UpdatePricingFile(current, target.Pricing, provider.Models, opts, now)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if len(update.Added)+len(update.Changed)+update.Unchanged == 0 {
			return fmt.Errorf("models.dev lists no priced %s models; keeping the current prices", target.Provider)
		}
		results = append(results, result{path, update})
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	refused := 0
	for _, r := range results {
		u := r.update
		if err := writeFileAtomic(r.path, pricing.FormatPricingFile(u.Data, u.Order)); err != nil {
			return err
		}
		fmt.Fprintf(out, "Wrote %s: %d added, %d changed, %d unchanged, %d kept (not on models.dev)\n",
			r.path, len(u.Added), len(u.Changed), u.Unchanged, len(u.Kept))
		for _, line := range u.Added {
			fmt.Fprintf(out, "  + %s\n", line)
		}
		for _, line := range u.Changed {
			fmt.Fprintf(out, "  ~ %s\n", line)
		}
		for _, key := range u.Deprecated {
			fmt.Fprintf(out, "  deprecated: %s\n", key)
		}
		for _, line := range u.Refused {
			fmt.Fprintf(out, "  ! refused (more than 10x): %s\n", line)
		}
		for _, line := range u.Notes {
			fmt.Fprintf(out, "  note: %s\n", line)
		}
		refused += len(u.Refused)
	}
	if refused > 0 {
		fmt.Fprintf(out, "%d price changes were refused; check them on the providers' pricing pages and rerun with --allow-large-changes if they are right.\n", refused)
	}
	if pricingDir != "" && sameDir(dir, pricingDir) {
		fmt.Fprintln(out, "AI Observer uses the new prices from its next start; stored costs keep their prices.")
	} else {
		fmt.Fprintln(out, "To use them, set AI_OBSERVER_PRICING_DIR to this directory (for backend/internal/pricing/data: rebuild).")
	}
	return nil
}

// currentPricingFile returns the pricing file that path replaces: the file
// at path, or the built-in file of that name when there is none
func currentPricingFile(path, name string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return pricing.EmbeddedPricingFile(name)
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return data, nil
}

// writeFileAtomic replaces path with data, so readers never see a partial file
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	defer os.Remove(tmp.Name()) // nothing left to remove after the rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func sameDir(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return absA == absB
}
