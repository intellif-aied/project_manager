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

	"github.com/aidashboard/api/internal/reporteval"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "validate":
		validateCommand(os.Args[2:])
	case "validate-association":
		validateAssociationCommand(os.Args[2:])
	case "evaluate-association":
		evaluateAssociationCommand(os.Args[2:])
	case "freeze-source":
		freezeSourceCommand(os.Args[2:])
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

func evaluateAssociationCommand(arguments []string) {
	flags := flag.NewFlagSet("evaluate-association", flag.ExitOnError)
	manifestPath := flags.String("manifest", "", "Project Association dataset manifest JSON")
	candidatesPath := flags.String("candidates", "", "candidate Brief workstream subjects JSON")
	_ = flags.Parse(arguments)
	if strings.TrimSpace(*manifestPath) == "" || strings.TrimSpace(*candidatesPath) == "" {
		fatal(fmt.Errorf("manifest and candidates are required"))
	}
	dataset, err := reporteval.LoadProjectAssociationDataset(*manifestPath)
	if err != nil {
		fatal(err)
	}
	candidates, err := reporteval.LoadProjectAssociationCandidates(*candidatesPath)
	if err != nil {
		fatal(err)
	}
	result, err := reporteval.EvaluateProjectAssociation(dataset, candidates)
	if err != nil {
		fatal(err)
	}
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fatal(err)
	}
	_, _ = os.Stdout.Write(append(payload, '\n'))
	if !result.Passed {
		os.Exit(1)
	}
}

func validateAssociationCommand(arguments []string) {
	flags := flag.NewFlagSet("validate-association", flag.ExitOnError)
	manifestPath := flags.String("manifest", "", "Project Association dataset manifest JSON")
	_ = flags.Parse(arguments)
	if strings.TrimSpace(*manifestPath) == "" {
		fatal(fmt.Errorf("manifest is required"))
	}
	dataset, err := reporteval.LoadProjectAssociationDataset(*manifestPath)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("valid Project Association dataset: %s (%d cases)\n", dataset.DatasetVersion, len(dataset.Cases))
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
	fmt.Printf("valid plan: %d cases, %d variants, dataset_sha256=%s\n", len(dataset.Manifest.Cases), len(plan.Variants), dataset.DatasetSHA256)
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }
func (values *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("slice key cannot be empty")
	}
	*values = append(*values, value)
	return nil
}

func freezeSourceCommand(arguments []string) {
	flags := flag.NewFlagSet("freeze-source", flag.ExitOnError)
	baseURL := flags.String("base-url", "", "isolated test server base URL")
	tokenEnv := flags.String("token-env", "", "environment variable containing the test account token")
	reportDate := flags.String("report-date", "", "personal daily report date (YYYY-MM-DD)")
	output := flags.String("output", "", "new source evidence JSON file")
	requestTimeout := flags.Duration("request-timeout", 5*time.Minute, "source freeze HTTP request timeout")
	pollInterval := flags.Duration("poll-interval", 2*time.Second, "HTTP runtime polling interval")
	var sliceKeys stringList
	flags.Var(&sliceKeys, "slice-key", "selected session slice UUID; repeat for multiple slices")
	_ = flags.Parse(arguments)
	if strings.TrimSpace(*baseURL) == "" || strings.TrimSpace(*tokenEnv) == "" || strings.TrimSpace(*reportDate) == "" || strings.TrimSpace(*output) == "" || len(sliceKeys) == 0 {
		fatal(fmt.Errorf("base-url, token-env, report-date, output, and at least one slice-key are required"))
	}
	if *requestTimeout <= 0 {
		fatal(fmt.Errorf("request-timeout must be greater than zero"))
	}
	token := strings.TrimSpace(os.Getenv(*tokenEnv))
	if token == "" {
		fatal(fmt.Errorf("token environment variable %s is empty", *tokenEnv))
	}
	runtime := &reporteval.HTTPRuntime{
		BaseURL: strings.TrimRight(*baseURL, "/"), BearerToken: token, PollInterval: *pollInterval,
		RequestTimeout: *requestTimeout,
	}
	ctx := context.Background()
	attestation, err := runtime.Attest(ctx)
	if err != nil {
		fatal(err)
	}
	if err := attestation.Validate(); err != nil {
		fatal(fmt.Errorf("reject runtime: %w", err))
	}
	source, err := runtime.FreezeSource(ctx, *reportDate, sliceKeys)
	if err != nil {
		fatal(err)
	}
	payload, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		fatal(err)
	}
	payload = append(payload, '\n')
	if err := os.MkdirAll(filepath.Dir(*output), 0o750); err != nil {
		fatal(err)
	}
	file, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		fatal(err)
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		fatal(err)
	}
	if err := file.Close(); err != nil {
		fatal(err)
	}
	fmt.Printf("source frozen: %s sha256=%s source_identity=%s runtime=%s/%s\n",
		*output, reporteval.CanonicalBytesSHA256(payload), source.SourceIdentitySHA256,
		attestation.InstanceID, attestation.BuildRevision)
}

func runCommand(arguments []string) {
	flags := flag.NewFlagSet("run", flag.ExitOnError)
	planPath := flags.String("plan", "", "evaluation execution plan JSON")
	outputDir := flags.String("output", "", "new evaluation bundle directory")
	pollInterval := flags.Duration("poll-interval", 2*time.Second, "run status poll interval")
	_ = flags.Parse(arguments)
	plan, dataset, err := loadInputs(*planPath)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(*outputDir) == "" {
		fatal(fmt.Errorf("output is required"))
	}
	if _, err := os.Stat(*outputDir); err == nil {
		fatal(fmt.Errorf("output directory already exists: %s", *outputDir))
	} else if !os.IsNotExist(err) {
		fatal(err)
	}
	exporter := reporteval.Exporter{OutputDir: *outputDir}
	_, err = exporter.Initialize(dataset)
	if err != nil {
		fatal(err)
	}
	runner := reporteval.Runner{RuntimeFactory: reporteval.NewHTTPRuntimeFactory(os.Getenv, nil, *pollInterval)}
	ctx := context.Background()
	receipts, err := runner.Execute(ctx, dataset, plan, func(
		_ context.Context, item reporteval.EvaluationCase, variant reporteval.VariantSpec,
		receipt *reporteval.RunReceipt, artifacts reporteval.RunArtifactEnvelope,
	) error {
		return exporter.ExportRun(item, dataset.Sources[item.CaseID], variant, receipt, artifacts)
	})
	if err != nil {
		fatal(err)
	}
	if err := exporter.Finalize(dataset, plan, receipts); err != nil {
		fatal(err)
	}
	fmt.Printf("bundle complete: %s (%d runs)\n", *outputDir, len(receipts))
}

func loadInputs(planPath string) (reporteval.ExecutionPlan, reporteval.FrozenDataset, error) {
	var plan reporteval.ExecutionPlan
	if strings.TrimSpace(planPath) == "" {
		return plan, reporteval.FrozenDataset{}, fmt.Errorf("plan is required")
	}
	if err := decodeFile(planPath, &plan); err != nil {
		return plan, reporteval.FrozenDataset{}, err
	}
	if err := plan.Validate(); err != nil {
		return plan, reporteval.FrozenDataset{}, fmt.Errorf("invalid plan: %w", err)
	}
	datasetPath := plan.DatasetFile
	if !filepath.IsAbs(datasetPath) {
		datasetPath = filepath.Join(filepath.Dir(planPath), datasetPath)
	}
	dataset, err := reporteval.LoadFrozenDataset(datasetPath)
	if err != nil {
		return plan, dataset, err
	}
	if err := plan.ValidateForDataset(dataset.Manifest); err != nil {
		return plan, dataset, fmt.Errorf("plan credentials do not match dataset: %w", err)
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
	fmt.Fprintln(os.Stderr, "usage: daily-report-eval <validate|validate-association|evaluate-association|freeze-source|run|verify|prepare-review|aggregate> [flags]")
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
