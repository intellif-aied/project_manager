function plainMarkdown(value: string) {
  return value
    .replace(/!\[[^\]]*\]\([^)]*\)/g, "")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/^\s{0,3}#{1,6}\s+/gm, "")
    .replace(/^\s*>\s?/gm, "")
    .replace(/^\s*(?:[-*+] |\d+\.\s+)/gm, "")
    .replace(/<[^>]+>/g, "")
    .replace(/[`*_~]/g, "")
    .replace(/\s+/g, " ")
    .trim();
}

function firstMarkdownParagraph(value: string) {
  for (const block of value.split(/\n\s*\n/)) {
    const paragraph = plainMarkdown(block);
    if (paragraph && !/^工作总结[：:]?$/.test(paragraph)) return paragraph;
  }
  return "";
}

function compactMarkdownBody(value: string) {
  const withoutHeadings = value
    .replace(/^\s{0,3}#{1,6}\s+.*$/gm, "")
    .replace(/^\s*(?:```.*|~~~.*)$/gm, "")
    .replace(/^\s*(?:-{3,}|\*{3,}|_{3,})\s*$/gm, "");
  return plainMarkdown(withoutHeadings) || plainMarkdown(value);
}

export function reportContentSummary(value: string) {
  const lines = value.replace(/\r\n?/g, "\n").split("\n");
  const summaryHeading = lines.findIndex((line) => /^\s{0,3}#{1,6}\s+工作总结[：:]?\s*#*\s*$/.test(line));
  if (summaryHeading >= 0) {
    const nextHeadingOffset = lines.slice(summaryHeading + 1).findIndex((line) => /^\s{0,3}#{1,6}\s+/.test(line));
    const sectionEnd = nextHeadingOffset >= 0 ? summaryHeading + 1 + nextHeadingOffset : lines.length;
    const summary = firstMarkdownParagraph(lines.slice(summaryHeading + 1, sectionEnd).join("\n"));
    if (summary) return summary;
  }
  return compactMarkdownBody(value);
}
