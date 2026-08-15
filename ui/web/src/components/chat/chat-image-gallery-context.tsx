import { createContext, useContext, useState, useMemo, useCallback } from "react";
import { ImageLightbox, type LightboxImage } from "@/components/shared/image-lightbox";

interface ChatImageGalleryCtx {
  /** Open the lightbox at the image matching this src URL. */
  openImage: (src: string) => void;
  /** Register images so they appear in the gallery. Called by child components. */
  allImages: LightboxImage[];
}

const Ctx = createContext<ChatImageGalleryCtx>({ openImage: () => {}, allImages: [] });

export const useChatImageGallery = () => useContext(Ctx);

interface Props {
  images: LightboxImage[];
  children: React.ReactNode;
}

/** Wraps chat thread content; provides a conversation-wide image lightbox. */
export function ChatImageGalleryProvider({ images, children }: Props) {
  const [currentIndex, setCurrentIndex] = useState<number | null>(null);

  const openImage = useCallback(
    (src: string) => {
      // Normalize: strip query params for matching
      const clean = (s: string) => s.split("?")[0] ?? s;
      const idx = images.findIndex((img) => clean(img.src) === clean(src));
      setCurrentIndex(idx >= 0 ? idx : null);
    },
    [images],
  );

  const value = useMemo(() => ({ openImage, allImages: images }), [openImage, images]);

  return (
    <Ctx.Provider value={value}>
      {children}
      {currentIndex !== null && images[currentIndex] && (
        <ImageLightbox
          src={images[currentIndex]!.src}
          alt={images[currentIndex]!.alt}
          fileName={images[currentIndex]!.fileName}
          size={images[currentIndex]!.size}
          onClose={() => setCurrentIndex(null)}
          images={images}
          currentIndex={currentIndex}
          onNavigate={setCurrentIndex}
        />
      )}
    </Ctx.Provider>
  );
}
