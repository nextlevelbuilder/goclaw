import * as React from "react"
import { Slider as SliderPrimitive } from "radix-ui"

interface SliderProps extends React.ComponentProps<typeof SliderPrimitive.Root> {
  className?: string
}

/**
 * Slider — Radix UI wrapper for the desktop (Lite) frontend.
 * Styling matches the web UI primitive without requiring the `cn` utility.
 */
function Slider({ className, ...props }: SliderProps) {
  return (
    <SliderPrimitive.Root
      data-slot="slider"
      className={[
        "relative flex w-full touch-none select-none items-center",
        "data-[disabled]:opacity-50 data-[disabled]:cursor-not-allowed",
        className ?? "",
      ].join(" ")}
      {...props}
    >
      <SliderPrimitive.Track
        data-slot="slider-track"
        className="bg-surface-tertiary relative h-1.5 w-full grow overflow-hidden rounded-full"
      >
        <SliderPrimitive.Range
          data-slot="slider-range"
          className="bg-accent absolute h-full"
        />
      </SliderPrimitive.Track>
      <SliderPrimitive.Thumb
        data-slot="slider-thumb"
        className="border-accent bg-white block size-4 shrink-0 rounded-full border shadow focus-visible:ring-2 focus-visible:ring-accent focus-visible:outline-none disabled:pointer-events-none disabled:opacity-50 transition-colors"
      />
    </SliderPrimitive.Root>
  )
}

export { Slider }
