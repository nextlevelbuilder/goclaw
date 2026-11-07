import { useEffect, useState } from 'react'

/** Below this window width the sidebar collapses to the icon rail. */
export const RAIL_BREAKPOINT_PX = 1100

function isNarrow(): boolean {
  return typeof window !== 'undefined' && window.innerWidth < RAIL_BREAKPOINT_PX
}

/** True while the window is narrower than the rail breakpoint. */
export function useNarrowWindow(): boolean {
  const [narrow, setNarrow] = useState(isNarrow)

  useEffect(() => {
    const onResize = () => setNarrow(isNarrow())
    onResize()
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  return narrow
}
