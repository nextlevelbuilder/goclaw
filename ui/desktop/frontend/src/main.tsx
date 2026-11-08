import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { MotionConfig } from 'framer-motion'
import './index.css'
import App from './App'
import { applyTheme, resolveTheme } from './lib/theme'
import { useUiStore } from './stores/ui-store'

// Paint the persisted (or OS) theme before the first React render so the
// window never flashes the wrong palette. App keeps it in sync afterwards.
applyTheme(resolveTheme(useUiStore.getState().theme))

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <MotionConfig reducedMotion="user">
      <App />
    </MotionConfig>
  </StrictMode>,
)
