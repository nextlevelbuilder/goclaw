export interface TreeNode {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  hasChildren?: boolean;
  loading?: boolean;
  protected?: boolean;
  children: TreeNode[];
}

export const CODE_EXTENSIONS = new Set([
  "ts", "tsx", "js", "jsx", "py", "go", "json", "yaml", "yml", "toml",
  "sh", "bash", "css", "html", "sql", "rs", "rb", "java", "kt", "swift",
  "c", "cpp", "h", "xml", "graphql", "proto", "lua", "zig", "env",
]);

export const IMAGE_EXTENSIONS = new Set([
  "jpg", "jpeg", "png", "gif", "webp", "svg", "ico", "bmp",
]);

const TEXT_EXTENSIONS = new Set([
  ...CODE_EXTENSIONS, ...["md", "csv", "txt", "log", "cfg", "ini", "conf"],
]);

export function extOf(name: string): string {
  const dot = name.lastIndexOf(".");
  return dot >= 0 ? name.slice(dot + 1).toLowerCase() : "";
}

export function isImageFile(name: string): boolean {
  return IMAGE_EXTENSIONS.has(extOf(name));
}

export function isTextFile(name: string): boolean {
  return TEXT_EXTENSIONS.has(extOf(name));
}

export function langFor(ext: string): string {
  const map: Record<string, string> = {
    ts: "typescript", tsx: "tsx", js: "javascript", jsx: "jsx",
    py: "python", go: "go", rs: "rust", rb: "ruby", sh: "bash", bash: "bash",
    yml: "yaml", yaml: "yaml", json: "json", toml: "toml",
    css: "css", html: "html", sql: "sql", xml: "xml",
    java: "java", kt: "kotlin", swift: "swift",
    c: "c", cpp: "cpp", h: "c", graphql: "graphql",
    proto: "protobuf", lua: "lua", zig: "zig",
  };
  return map[ext] ?? ext;
}

/** Returns badge variant based on file size. */
export function sizeBadgeVariant(bytes: number): "outline" | "info" | "warning" | "destructive" {
  if (bytes < 100 * 1024) return "outline";
  if (bytes < 1024 * 1024) return "info";
  if (bytes < 10 * 1024 * 1024) return "warning";
  return "destructive";
}

interface TreeInput {
  path: string;
  name: string;
  isDir: boolean;
  size: number;
  hasChildren?: boolean;
  protected?: boolean;
}

export function buildTree(files: TreeInput[]): TreeNode[] {
  const root: TreeNode[] = [];
  const dirMap = new Map<string, TreeNode>();

  const sorted = [...files].sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
    return a.path.localeCompare(b.path);
  });

  for (const f of sorted) {
    const node: TreeNode = {
      name: f.name,
      path: f.path,
      isDir: f.isDir,
      size: f.size,
      hasChildren: f.hasChildren,
      protected: f.protected,
      children: [],
    };

    if (f.isDir) {
      dirMap.set(f.path, node);
    }

    const parentPath = f.path.includes("/")
      ? f.path.slice(0, f.path.lastIndexOf("/"))
      : "";

    if (parentPath && dirMap.has(parentPath)) {
      dirMap.get(parentPath)!.children.push(node);
    } else {
      root.push(node);
    }
  }

  const sortChildren = (nodes: TreeNode[]) => {
    nodes.sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
    for (const n of nodes) {
      if (n.children.length > 0) sortChildren(n.children);
    }
  };
  sortChildren(root);
  return root;
}

/** Merges a loaded subtree into an existing tree, replacing the target folder's children. */
export function mergeSubtree(tree: TreeNode[], parentPath: string, newChildren: TreeInput[]): TreeNode[] {
  const childTree = buildTree(newChildren);

  const merge = (nodes: TreeNode[]): TreeNode[] =>
    nodes.map((node) => {
      if (node.path === parentPath && node.isDir) {
        return { ...node, children: childTree, hasChildren: false, loading: false };
      }
      if (node.children.length > 0) {
        return { ...node, children: merge(node.children) };
      }
      return node;
    });

  return merge(tree);
}

/** Marks a node as loading in the tree (immutable update). */
export function setNodeLoading(tree: TreeNode[], path: string, loading: boolean): TreeNode[] {
  return tree.map((node) => {
    if (node.path === path) return { ...node, loading };
    if (node.children.length > 0) return { ...node, children: setNodeLoading(node.children, path, loading) };
    return node;
  });
}

export function stripFrontmatter(content: string): string {
  if (!content.startsWith("---")) return content;
  const end = content.indexOf("---", 3);
  if (end < 0) return content;
  return content.slice(end + 3).replace(/^\n+/, "");
}

export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/** Convert a local file path to a /v1/files/ URL for serving.
 *  Extracts basename so the backend fallback search can find generated files. */
export function toFileUrl(path: string, token?: string): string {
  let url: string;
  if (path.startsWith("/v1/files/") || path.includes("/v1/files/")) {
    url = path;
  } else {
    const basename = path.split("/").pop() ?? path;
    url = `/v1/files/${basename}`;
  }
  if (token) {
    const sep = url.includes("?") ? "&" : "?";
    url += `${sep}token=${encodeURIComponent(token)}`;
  }
  return url;
}

/** Determine MediaItem kind from MIME type */
export function mediaKindFromMime(mime: string): "image" | "video" | "audio" | "document" | "code" {
  if (mime.startsWith("image/")) return "image";
  if (mime.startsWith("video/")) return "video";
  if (mime.startsWith("audio/")) return "audio";
  const ext = mime.split("/").pop() ?? "";
  if (["javascript", "typescript", "python", "json", "xml", "html", "css"].some((t) => ext.includes(t))) return "code";
  return "document";
}
