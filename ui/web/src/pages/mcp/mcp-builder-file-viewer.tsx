import { useTranslation } from "react-i18next";
import { BuilderFileViewer } from "@/components/shared/builder-file-viewer";

interface MCPBuilderFileViewerProps {
  content: string | null;
  path: string | null;
  loading: boolean;
  onClose: () => void;
}

export function MCPBuilderFileViewer({
  content,
  path,
  loading,
  onClose,
}: MCPBuilderFileViewerProps) {
  const { t } = useTranslation("mcp");
  const fileName = path?.includes("/") ? path.slice(path.lastIndexOf("/") + 1) : path;

  return (
    <BuilderFileViewer
      content={content}
      path={path}
      loading={loading}
      onClose={onClose}
      closeLabel={t("builder.closeFile")}
      fileLoadError={t("builder.fileLoadError", { name: fileName })}
    />
  );
}
