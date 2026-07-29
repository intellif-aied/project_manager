package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	projectdb "github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/reporteval"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "validate":
		validateCommand(os.Args[2:])
	case "run":
		runCommand(os.Args[2:])
	case "verify":
		verifyCommand(os.Args[2:])
	case "prepare-review":
		prepareReviewCommand(os.Args[2:])
	case "aggregate":
		aggregateCommand(os.Args[2:])
	default:
		usage()
	}
}

func aggregateCommand(arguments []string) {
	flags := flag.NewFlagSet("aggregate", flag.ExitOnError)
	bundleDir := flags.String("bundle", "", "verified evaluation bundle directory")
	aiReviews := flags.String("ai-reviews", "", "resolved AI case-results JSONL")
	goldReviews := flags.String("gold-reviews", "", "optional Gold Review JSONL")
	outputDir := flags.String("output", "", "evaluation result directory")
	_ = flags.Parse(arguments)
	if *bundleDir == "" || *aiReviews == "" || *outputDir == "" {
		fatal(fmt.Errorf("bundle, ai-reviews, and output are required"))
	}
	result, err := reporteval.AggregateReviews(*bundleDir, *aiReviews, *goldReviews)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(*outputDir, 0o750); err != nil {
		fatal(err)
	}
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*outputDir, "evaluation.json"), append(payload, '\n'), 0o640); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*outputDir, "report.md"), []byte(reporteval.RenderEvaluationMarkdown(result)), 0o640); err != nil {
		fatal(err)
	}
}

func prepareReviewCommand(arguments []string) {
	flags := flag.NewFlagSet("prepare-review", flag.ExitOnError)
	bundleDir := flags.String("bundle", "", "verified evaluation bundle directory")
	outputDir := flags.String("output", "", "new anonymous review workspace")
	_ = flags.Parse(arguments)
	if *bundleDir == "" || *outputDir == "" {
		fatal(fmt.Errorf("bundle and output are required"))
	}
	if err := reporteval.PrepareAnonymousReview(*bundleDir, *outputDir); err != nil {
		fatal(err)
	}
	fmt.Printf("anonymous review input ready: %s/review-input\n", *outputDir)
}

func verifyCommand(arguments []string) {
	flags := flag.NewFlagSet("verify", flag.ExitOnError)
	bundleDir := flags.String("bundle", "", "frozen evaluation bundle directory")
	output := flags.String("output", "", "optional verification JSON output")
	_ = flags.Parse(arguments)
	if strings.TrimSpace(*bundleDir) == "" {
		fatal(fmt.Errorf("bundle is required"))
	}
	result, err := reporteval.VerifyBundle(*bundleDir)
	if err != nil {
		fatal(err)
	}
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fatal(err)
	}
	payload = append(payload, '\n')
	if *output != "" {
		if err := os.WriteFile(*output, payload, 0o640); err != nil {
			fatal(err)
		}
	} else {
		_, _ = os.Stdout.Write(payload)
	}
	if !result.Valid {
		os.Exit(1)
	}
}

func validateCommand(arguments []string) {
	flags := flag.NewFlagSet("validate", flag.ExitOnError)
	planPath := flags.String("plan", "", "evaluation execution plan JSON")
	_ = flags.Parse(arguments)
	plan, dataset, err := loadInputs(*planPath)
	if err != nil {
		fatal(err)
	}
	datasetHash, err := reporteval.CanonicalSHA256(dataset)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("valid plan: %d cases, %d variants, dataset_sha256=%s\n", len(dataset.Cases), len(plan.Variants), datasetHash)
}

func runCommand(arguments []string) {
	flags := flag.NewFlagSet("run", flag.ExitOnError)
	planPath := flags.String("plan", "", "evaluation execution plan JSON")
	baseURL := flags.String("base-url", "", "test server base URL")
	tokenEnv := flags.String("token-env", "AIDA_EVAL_TOKEN", "environment variable containing a test account token")
	databaseURL := flags.String("database-url", os.Getenv("DATABASE_URL"), "test PostgreSQL connection URL")
	outputDir := flags.String("output", "", "new evaluation bundle directory")
	environment := flags.String("environment", "", "must be test; production execution is forbidden")
	pollInterval := flags.Duration("poll-interval", 2*time.Second, "run status poll interval")
	_ = flags.Parse(arguments)
	plan, dataset, err := loadInputs(*planPath)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(*databaseURL) == "" || strings.TrimSpace(*baseURL) == "" || strings.TrimSpace(*outputDir) == "" {
		fatal(fmt.Errorf("database-url, base-url, and output are required"))
	}
	if strings.TrimSpace(*environment) != "test" {
		fatal(fmt.Errorf("environment must be explicitly set to test"))
	}
	token := strings.TrimSpace(os.Getenv(*tokenEnv))
	if token == "" {
		fatal(fmt.Errorf("token environment variable %s is empty", *tokenEnv))
	}
	if _, err := os.Stat(*outputDir); err == nil {
		fatal(fmt.Errorf("output directory already exists: %s", *outputDir))
	} else if !os.IsNotExist(err) {
		fatal(err)
	}
	database, err := projectdb.Connect(*databaseURL)
	if err != nil {
		fatal(err)
	}
	defer database.Close()
	exporter := reporteval.Exporter{DB: database, OutputDir: *outputDir}
	datasetHash, err := exporter.Initialize(dataset)
	if err != nil {
		fatal(err)
	}
	runner := reporteval.Runner{
		BaseURL: *baseURL, BearerToken: token, PollInterval: *pollInterval,
	}
	ctx := context.Background()
	receipts, err := runner.Execute(ctx, dataset, plan, func(
		ctx context.Context, item reporteval.EvaluationCase, variant reporteval.VariantSpec, receipt *reporteval.RunReceipt,
	) error {
		return exporter.ExportRun(ctx, item, variant, receipt)
	})
	if err != nil {
		fatal(err)
	}
	if err := exporter.Finalize(dataset, plan, datasetHash, receipts); err != nil {
		fatal(err)
	}
	fmt.Printf("bundle complete: %s (%d runs)\n", *outputDir, len(receipts))
}

func loadInputs(planPath string) (reporteval.ExecutionPlan, reporteval.DatasetManifest, error) {
	var plan reporteval.ExecutionPlan
	if strings.TrimSpace(planPath) == "" {
		return plan, reporteval.DatasetManifest{}, fmt.Errorf("plan is required")
	}
	if err := decodeFile(planPath, &plan); err != nil {
		return plan, reporteval.DatasetManifest{}, err
	}
	if err := plan.Validate(); err != nil {
		return plan, reporteval.DatasetManifest{}, fmt.Errorf("invalid plan: %w", err)
	}
	datasetPath := plan.DatasetFile
	if !filepath.IsAbs(datasetPath) {
		datasetPath = filepath.Join(filepath.Dir(planPath), datasetPath)
	}
	var dataset reporteval.DatasetManifest
	if err := decodeFile(datasetPath, &dataset); err != nil {
		return plan, dataset, err
	}
	reporteval.NormalizeDataset(&dataset)
	if err := dataset.Validate(); err != nil {
		return plan, dataset, fmt.Errorf("invalid dataset: %w", err)
	}
	return plan, dataset, nil
}

func decodeFile(path string, output any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: daily-report-eval <validate|run|verify|prepare-review|aggregate> [flags]")
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
