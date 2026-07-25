import CodeMirror, {
  Decoration,
  EditorView,
  StateField,
  WidgetType,
  type DecorationSet,
  type EditorState,
  type ReactCodeMirrorRef
} from "@uiw/react-codemirror";
import { markdown } from "@codemirror/lang-markdown";
import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { tags } from "@lezer/highlight";
import "@fontsource-variable/noto-sans-sc";
import { useRef, useState } from "react";

import { feedback } from "@/shared/feedback/feedback";
import { api } from "@/shared/request/httpClient";

import "./MarkdownEditor.css";

const reportMarkdownHighlight = HighlightStyle.define([
  { tag: tags.heading, color: "#1e293b", fontWeight: "600", textDecoration: "none" },
  { tag: tags.meta, color: "#94a3b8" },
  { tag: tags.strong, color: "#1e293b", fontWeight: "600" },
  { tag: tags.emphasis, color: "#475569", fontStyle: "italic" },
  { tag: tags.link, color: "#2563eb", textDecoration: "none" },
  { tag: tags.url, color: "#64748b", textDecoration: "none" },
  { tag: tags.quote, color: "#475569" },
  { tag: tags.list, color: "#2563eb" },
  {
    tag: tags.monospace,
    color: "#be123c",
    fontFamily: "SFMono-Regular, Consolas, monospace"
  }
]);

const reportImagePattern = /!\[([^\]]*)\]\((\/api\/v1\/report-images\/[^)\s]+)\)/g;

class MarkdownImageWidget extends WidgetType {
  constructor(
    private readonly url: string,
    private readonly alt: string
  ) {
    super();
  }

  eq(other: MarkdownImageWidget) {
    return other.url === this.url && other.alt === this.alt;
  }

  toDOM() {
    const figure = document.createElement("figure");
    figure.className = "markdown-editor-inline-image";
    const image = document.createElement("img");
    image.src = this.url;
    image.alt = this.alt || "正文图片";
    image.loading = "lazy";
    figure.append(image);
    return figure;
  }

  ignoreEvent() {
    return true;
  }
}

function imagePreviewDecorations(state: EditorState): DecorationSet {
  const decorations = [];
  const value = state.doc.toString();
  for (const match of value.matchAll(reportImagePattern)) {
    if (match.index === undefined) continue;
    const line = state.doc.lineAt(match.index);
    decorations.push(
      Decoration.widget({
        widget: new MarkdownImageWidget(match[2], match[1]),
        block: true,
        side: 1
      }).range(line.to)
    );
  }
  return Decoration.set(decorations, true);
}

const markdownImagePreviews = StateField.define<DecorationSet>({
  create(state) {
    return imagePreviewDecorations(state);
  },
  update(decorations, transaction) {
    return transaction.docChanged ? imagePreviewDecorations(transaction.state) : decorations;
  },
  provide: (field) => EditorView.decorations.from(field)
});

interface MarkdownEditorProps {
  value: string;
  onChange: (value: string) => void;
  className?: string;
  height?: string;
  disabled?: boolean;
}

export function MarkdownEditor({
  value,
  onChange,
  className,
  height = "360px",
  disabled = false
}: MarkdownEditorProps) {
  const editorRef = useRef<ReactCodeMirrorRef>(null);
  const [uploadingImage, setUploadingImage] = useState(false);

  const uploadPastedImage = async (file: File) => {
    if (!["image/png", "image/jpeg", "image/webp"].includes(file.type)) {
      feedback.message()?.warning("仅支持 PNG、JPEG、WebP 图片");
      return;
    }
    if (file.size > 5 * 1024 * 1024) {
      feedback.message()?.warning("图片不能超过 5MB，请压缩后重试");
      return;
    }
    const view = editorRef.current?.view;
    if (!view || uploadingImage) return;
    const insertionPoint = view.state.selection.main.from;
    const form = new FormData();
    form.append("file", file, file.name || "pasted-image.png");
    setUploadingImage(true);
    try {
      const response = await api.post<{ url: string }>("/report-images", form, {
        headers: { "Content-Type": "multipart/form-data" }
      });
      const url = response.data.url;
      view.dispatch({ changes: { from: insertionPoint, insert: `\n![粘贴图片](${url})\n` } });
      feedback.message()?.success("图片已上传并插入正文");
    } catch {
      feedback.message()?.error("图片上传失败，请稍后重试");
    } finally {
      setUploadingImage(false);
    }
  };

  return (
    <div
      className={`markdown-editor-shell${className ? ` ${className}` : ""}`}
      style={{ height }}
    >
      <CodeMirror
        ref={editorRef}
        className="markdown-editor"
        value={value}
        height="100%"
        extensions={[
          markdown(),
          EditorView.lineWrapping,
          markdownImagePreviews,
          syntaxHighlighting(reportMarkdownHighlight)
        ]}
        editable={!disabled && !uploadingImage}
        basicSetup={{ lineNumbers: false, foldGutter: false, highlightActiveLine: false }}
        placeholder="支持 Markdown；可直接粘贴图片。"
        onChange={onChange}
        onPaste={(event) => {
          const image = [...event.clipboardData.items].find((item) => item.type.startsWith("image/"))?.getAsFile();
          if (!image) return;
          event.preventDefault();
          void uploadPastedImage(image);
        }}
      />
      {uploadingImage ? <div className="markdown-editor-uploading" aria-live="polite">图片上传中…</div> : null}
    </div>
  );
}
