import { useEffect, useRef } from "react";
import Viewer from "@toast-ui/editor/viewer";
import "@toast-ui/editor/toastui-editor-viewer.css";
import "@fontsource-variable/noto-sans-sc";

import "./MarkdownViewer.css";

interface MarkdownViewerProps {
  value: string;
}

export function MarkdownViewer({ value }: MarkdownViewerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const viewerRef = useRef<Viewer | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    container.innerHTML = "";
    const viewer = new Viewer({ el: container });
    viewerRef.current = viewer;
    return () => {
      viewer.destroy();
      viewerRef.current = null;
      container.innerHTML = "";
    };
  }, []);

  useEffect(() => {
    viewerRef.current?.setMarkdown(value);
  }, [value]);

  return <div ref={containerRef} className="markdown-viewer" />;
}
