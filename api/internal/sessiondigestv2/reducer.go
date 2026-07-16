package sessiondigestv2

import (
	"bufio"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

var exitCodePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)process exited with code\s+(-?[0-9]+)`),
	regexp.MustCompile(`(?i)"exit_code"\s*:\s*(-?[0-9]+)`),
	regexp.MustCompile(`(?i)\bexit code\s+(-?[0-9]+)`),
}

type ReducedCommand struct {
	Recognized    bool
	CommandFamily string
	Kind          string
	Status        string
	ExitCode      *int
	Summary       string
}

func ReduceCommandOutput(command, output string) ReducedCommand {
	family, kind := classifyCommand(command)
	result := ReducedCommand{
		Recognized:    family != "",
		CommandFamily: family,
		Kind:          kind,
		Status:        "unknown",
		Summary:       "结果状态无法可靠判断",
	}
	if code, ok := lastExitCode(output); ok {
		result.Recognized = true
		result.ExitCode = &code
		if code == 0 {
			result.Status = "passed"
			result.Summary = "命令执行成功"
		} else {
			result.Status = "failed"
			result.Summary = "命令执行失败"
		}
		return result
	}
	if family == "go test" {
		if status, summary, ok := reduceGoTestJSON(output); ok {
			result.Recognized = true
			result.Status = status
			result.Summary = summary
		}
	}
	return result
}

func classifyCommand(command string) (string, string) {
	lower := strings.ToLower(strings.Join(strings.Fields(command), " "))
	candidates := []struct {
		needles []string
		family  string
		kind    string
	}{
		{[]string{"go test"}, "go test", "validation"},
		{[]string{"go vet"}, "go vet", "validation"},
		{[]string{"golangci-lint"}, "golangci-lint", "validation"},
		{[]string{"pnpm typecheck"}, "pnpm typecheck", "validation"},
		{[]string{"pnpm lint"}, "pnpm lint", "validation"},
		{[]string{"pnpm build"}, "pnpm build", "validation"},
		{[]string{"pnpm test"}, "pnpm test", "validation"},
		{[]string{"npm run build"}, "npm run build", "validation"},
		{[]string{"npm test"}, "npm test", "validation"},
		{[]string{"pytest"}, "pytest", "validation"},
		{[]string{"cargo test"}, "cargo test", "validation"},
		{[]string{"make test"}, "make test", "validation"},
		{[]string{"git commit"}, "git commit", "commit"},
		{[]string{"git status"}, "git status", "repository"},
		{[]string{"git diff"}, "git diff", "repository"},
		{[]string{"docker compose", "docker build", "docker push"}, "docker", "runtime_change"},
		{[]string{"curl ", "wget "}, "http check", "api_check"},
	}
	for _, candidate := range candidates {
		for _, needle := range candidate.needles {
			if strings.Contains(lower, needle) {
				return candidate.family, candidate.kind
			}
		}
	}
	return "", "process"
}

func lastExitCode(output string) (int, bool) {
	var (
		code  int
		found bool
	)
	for _, pattern := range exitCodePatterns {
		for _, match := range pattern.FindAllStringSubmatch(output, -1) {
			parsed, err := strconv.Atoi(match[1])
			if err == nil {
				code = parsed
				found = true
			}
		}
	}
	return code, found
}

func reduceGoTestJSON(output string) (string, string, bool) {
	type goTestEvent struct {
		Action  string `json:"Action"`
		Package string `json:"Package"`
		Test    string `json:"Test"`
	}
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	recognized := 0
	passed := 0
	failed := 0
	skipped := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var event goTestEvent
		if json.Unmarshal([]byte(line), &event) != nil || event.Action == "" ||
			(event.Package == "" && event.Test == "") {
			continue
		}
		recognized++
		switch event.Action {
		case "pass":
			passed++
		case "fail":
			failed++
		case "skip":
			skipped++
		}
	}
	if scanner.Err() != nil || recognized == 0 {
		return "", "", false
	}
	status := "passed"
	if failed > 0 {
		status = "failed"
	}
	return status, "Go test：通过 " + strconv.Itoa(passed) +
		"，失败 " + strconv.Itoa(failed) + "，跳过 " + strconv.Itoa(skipped), true
}
