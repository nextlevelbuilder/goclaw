const EXT_BY_MIME: Record<string, string> = {
  "image/jpeg": "jpg",
  "image/svg+xml": "svg",
};

/**
 * Image files carried by a paste event. Clipboard images usually arrive with a
 * generic name ("image.png") or none at all, so unnamed ones get a unique
 * "pasted-<timestamp>.<ext>" name. Returns [] for text-only pastes.
 */
export function extractPastedImages(data: DataTransfer | null, now: number = Date.now()): File[] {
  if (!data) return [];
  const images: File[] = [];
  for (const item of Array.from(data.items ?? [])) {
    if (item.kind !== "file" || !item.type.startsWith("image/")) continue;
    const file = item.getAsFile();
    if (!file) continue;
    if (file.name) {
      images.push(file);
      continue;
    }
    const subtype = item.type.slice("image/".length);
    const ext = EXT_BY_MIME[item.type] ?? subtype.replace(/\+.*$/, "");
    const name = `pasted-${now}${images.length > 0 ? `-${images.length}` : ""}.${ext}`;
    images.push(new File([file], name, { type: file.type, lastModified: now }));
  }
  return images;
}
