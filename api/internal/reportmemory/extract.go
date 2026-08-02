package reportmemory

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	themeLimit     = 8
	childLimit     = 10
	titleRuneLimit = 160
)

var (
	listLinePattern = regexp.MustCompile(`^(\s*)(?:[-+*]|\d+[.)])\s+(.+?)\s*$`)
	linkPattern     = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
)

type Theme struct {
	Title    string
	Children []string
}

type parsedLine struct {
	indent int
	text   string
}

func ExtractThemes(content string) []Theme {
	content = normalizeMarkdown(content)
	if overview := overviewSection(content); overview != "" {
		if themes := themesFromLists(overview); len(themes) > 0 {
			return themes
		}
	}
	if themes := themesFromLists(content); len(themes) > 0 {
		return themes
	}
	return themesFromPlainText(content)
}

func normalizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var result strings.Builder
	for _, current := range value {
		if unicode.IsLetter(current) || unicode.IsNumber(current) {
			result.WriteRune(current)
		}
	}
	return result.String()
}

func normalizeMarkdown(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	if !strings.Contains(content, "\n") && strings.Contains(content, `\n`) {
		content = strings.ReplaceAll(content, `\n`, "\n")
	}
	return strings.TrimSpace(content)
}

func overviewSection(content string) string {
	return markdownSection(content, "工作概览")
}

func markdownSection(content, heading string) string {
	lines := strings.Split(content, "\n")
	start := -1
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") && strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")) == heading {
			start = index + 1
			continue
		}
		if start >= 0 && strings.HasPrefix(trimmed, "## ") {
			return strings.Join(lines[start:index], "\n")
		}
	}
	if start >= 0 {
		return strings.Join(lines[start:], "\n")
	}
	return ""
}

func themesFromLists(content string) []Theme {
	parsed := make([]parsedLine, 0)
	for _, line := range strings.Split(content, "\n") {
		match := listLinePattern.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		text := sanitizeTitle(match[2])
		if text != "" {
			parsed = append(parsed, parsedLine{indent: indentation(match[1]), text: text})
		}
	}
	if len(parsed) == 0 {
		return nil
	}
	rootIndent := parsed[0].indent
	for _, line := range parsed[1:] {
		if line.indent < rootIndent {
			rootIndent = line.indent
		}
	}
	themes := make([]Theme, 0, themeLimit)
	current, childIndent := -1, -1
	for _, line := range parsed {
		switch {
		case line.indent == rootIndent:
			if len(themes) >= themeLimit {
				continue
			}
			if findTheme(themes, line.text) >= 0 {
				current = findTheme(themes, line.text)
				continue
			}
			themes = append(themes, Theme{Title: line.text})
			current, childIndent = len(themes)-1, -1
		case line.indent > rootIndent && current >= 0:
			if childIndent < 0 {
				childIndent = line.indent
			}
			if line.indent == childIndent && len(themes[current].Children) < childLimit && line.text != themes[current].Title {
				themes[current].Children = appendUnique(themes[current].Children, line.text)
			}
		}
	}
	return themes
}

func themesFromHeadings(content string) []Theme {
	result := make([]Theme, 0, themeLimit)
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "### ") {
			continue
		}
		title := sanitizeTitle(strings.TrimPrefix(trimmed, "### "))
		if title == "" || title == "工作概览" || title == "工作详情" || findTheme(result, title) >= 0 {
			continue
		}
		result = append(result, Theme{Title: title})
		if len(result) >= themeLimit {
			break
		}
	}
	return result
}

func themesFromPlainText(content string) []Theme {
	for _, line := range strings.Split(content, "\n") {
		title := sanitizeTitle(line)
		if title != "" && !strings.HasPrefix(title, "## ") {
			return []Theme{{Title: title}}
		}
	}
	return nil
}

func sanitizeTitle(value string) string {
	value = linkPattern.ReplaceAllString(value, "$1")
	value = strings.TrimSpace(strings.Trim(value, "#*_` "))
	if value == "" || strings.HasPrefix(value, "[图片]") || strings.HasPrefix(value, "粘贴图片") {
		return ""
	}
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) > titleRuneLimit {
		value = strings.TrimSpace(string([]rune(value)[:titleRuneLimit]))
	}
	return value
}

func indentation(value string) int { return len(strings.ReplaceAll(value, "\t", "    ")) }

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func findTheme(themes []Theme, title string) int {
	for index, theme := range themes {
		if theme.Title == title {
			return index
		}
	}
	return -1
}
