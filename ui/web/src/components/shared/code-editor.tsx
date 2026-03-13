import { useMemo } from "react";
import CodeMirror from "@uiw/react-codemirror";
import { python } from "@codemirror/lang-python";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { oneDark } from "@codemirror/theme-one-dark";
import { EditorState } from "@uiw/react-codemirror";
import { cn } from "@/lib/utils";

export type CodeLanguage = "python" | "javascript" | "typescript" | "json" | "text";

interface CodeEditorProps {
  value: string;
  onChange: (value: string) => void;
  language?: CodeLanguage;
  readOnly?: boolean;
  className?: string;
}

export function CodeEditor({ value, onChange, language, readOnly, className }: CodeEditorProps) {
  const extensions = useMemo(() => {
    const exts = [];
    switch (language) {
      case "python":
        exts.push(python());
        break;
      case "javascript":
        exts.push(javascript());
        break;
      case "typescript":
        exts.push(javascript({ typescript: true }));
        break;
      case "json":
        exts.push(json());
        break;
    }
    if (readOnly) exts.push(EditorState.readOnly.of(true));
    return exts;
  }, [language, readOnly]);

  return (
    <CodeMirror
      value={value}
      onChange={onChange}
      extensions={extensions}
      theme={oneDark}
      height="100%"
      className={cn("h-full overflow-auto text-sm", className)}
      basicSetup={{
        lineNumbers: true,
        highlightActiveLine: true,
        bracketMatching: true,
        autocompletion: true,
        history: true,
        foldGutter: true,
        searchKeymap: true,
      }}
    />
  );
}
