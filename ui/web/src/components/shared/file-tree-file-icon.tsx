/**
 * File icon component for the file tree — maps file extensions to Lucide icons.
 */
import {
  FileText,
  FileCode2,
  File,
  FileImage,
  FileJson2,
  FileSpreadsheet,
  FileTerminal,
  FileArchive,
  FileVideo,
  FileAudio,
  FileCog,
  FileType,
  FileLock,
} from "lucide-react";
import { extOf, CODE_EXTENSIONS, IMAGE_EXTENSIONS } from "@/lib/file-helpers";

const cls = "h-4 w-4 shrink-0";

export function FileIcon({ name }: { name: string }) {
  const ext = extOf(name);
  if (ext === "md" || ext === "mdx") return <FileText className={`${cls} text-blue-500`} />;
  if (ext === "json" || ext === "json5") return <FileJson2 className={`${cls} text-yellow-600`} />;
  if (ext === "yaml" || ext === "yml" || ext === "toml") return <FileCog className={`${cls} text-orange-500`} />;
  if (ext === "csv") return <FileSpreadsheet className={`${cls} text-green-600`} />;
  if (ext === "sh" || ext === "bash" || ext === "zsh") return <FileTerminal className={`${cls} text-lime-600`} />;
  if (IMAGE_EXTENSIONS.has(ext)) return <FileImage className={`${cls} text-emerald-500`} />;
  if (ext === "mp4" || ext === "webm" || ext === "mov" || ext === "avi" || ext === "mkv") return <FileVideo className={`${cls} text-pink-500`} />;
  if (ext === "mp3" || ext === "wav" || ext === "ogg" || ext === "flac" || ext === "m4a") return <FileAudio className={`${cls} text-orange-500`} />;
  if (ext === "zip" || ext === "tar" || ext === "gz" || ext === "rar" || ext === "7z" || ext === "bz2") return <FileArchive className={`${cls} text-amber-600`} />;
  if (ext === "ttf" || ext === "otf" || ext === "woff" || ext === "woff2") return <FileType className={`${cls} text-slate-500`} />;
  if (ext === "env" || ext === "pem" || ext === "key" || ext === "crt") return <FileLock className={`${cls} text-red-500`} />;
  if (CODE_EXTENSIONS.has(ext)) return <FileCode2 className={`${cls} text-orange-500`} />;
  return <File className={`${cls} text-muted-foreground`} />;
}
