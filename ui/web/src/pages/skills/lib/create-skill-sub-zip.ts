/** Utility for extracting a single skill's subdirectory into a standalone ZIP file. */
import JSZip from "jszip";

/**
 * Extract files under `dir/` from a pre-parsed JSZip instance, strip the
 * directory prefix, and return a new File suitable for upload.
 *
 * Accepts a JSZip instance (not a File) so the caller can parse the original
 * ZIP once and reuse it across multiple skill extractions — O(N) instead of
 * O(N*M) where M is ZIP parse cost.
 */
export async function createSkillSubZip(zip: JSZip, dir: string): Promise<File> {
  const sub = new JSZip();
  const prefix = dir + "/";

  for (const [path, entry] of Object.entries(zip.files)) {
    if (path.startsWith(prefix) && !entry.dir) {
      const relativePath = path.slice(prefix.length);
      if (relativePath) {
        sub.file(relativePath, await entry.async("blob"));
      }
    }
  }

  const blob = await sub.generateAsync({ type: "blob" });
  return new File([blob], `${dir}.zip`, { type: "application/zip" });
}
