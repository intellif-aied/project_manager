// Minimal ambient types for @toast-ui/editor's standalone viewer build.
// The package ships real typings under its `types/` dir, but its package.json
// `exports` map omits a `types` condition, so TS (moduleResolution: Bundler)
// cannot resolve them. We declare only what we use here.
declare module "@toast-ui/editor/viewer" {
  export interface ViewerOptions {
    el: HTMLElement;
    initialValue?: string;
  }

  export default class Viewer {
    constructor(options: ViewerOptions);
    setMarkdown(markdown: string): void;
    destroy(): void;
  }
}

declare module "@toast-ui/editor" {
  export interface EditorOptions {
    el: HTMLElement;
    height?: string;
    initialValue?: string;
    initialEditType?: "markdown" | "wysiwyg";
    previewStyle?: "tab" | "vertical";
    hideModeSwitch?: boolean;
    toolbarItems?: string[][];
    usageStatistics?: boolean;
    events?: {
      change?: () => void;
    };
  }

  export default class Editor {
    constructor(options: EditorOptions);
    getMarkdown(): string;
    setMarkdown(markdown: string): void;
    destroy(): void;
  }
}
