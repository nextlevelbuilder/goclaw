import { useTranslation } from 'react-i18next'
import { motion, AnimatePresence } from 'framer-motion'

const SUMMONING_FILE_KEYS = [
  { name: 'SOUL.md', required: true, labelKey: 'summoning.fileLabelSOUL' },
  { name: 'IDENTITY.md', required: true, labelKey: 'summoning.fileLabelIDENTITY' },
]

interface SummoningProgressStepsProps {
  generatedFiles: string[]
}

export function SummoningProgressSteps({ generatedFiles }: SummoningProgressStepsProps) {
  const { t } = useTranslation('desktop')

  return (
    <div className="w-full space-y-2">
      <AnimatePresence>
        {SUMMONING_FILE_KEYS.map((file, i) => {
          const done = generatedFiles.includes(file.name)
          return (
            <motion.div
              key={file.name}
              initial={{ opacity: 1 }}
              animate={{ opacity: 1 }}
              transition={{ duration: 0.3 }}
              className="flex items-center gap-3 rounded-md px-3 py-1.5"
            >
              <motion.div
                className={`flex h-5 w-5 items-center justify-center rounded-full text-xs ${
                  done
                    ? 'bg-orange-100 text-orange-600 dark:bg-orange-900/40 dark:text-orange-400'
                    : 'bg-surface-tertiary text-text-muted'
                }`}
                animate={done ? { scale: [0.8, 1.2, 1] } : {}}
                transition={{ duration: 0.3 }}
              >
                {done ? '✓' : i + 1}
              </motion.div>
              <div className="flex-1">
                <span className={`text-sm ${done ? 'text-text-primary font-medium' : 'text-text-secondary'}`}>
                  {file.name}
                </span>
                <span className="ml-2 text-xs text-text-muted">{t(`${file.labelKey}`)}</span>
              </div>
              {done && (
                <motion.span
                  initial={{ opacity: 0, scale: 0.5 }}
                  animate={{ opacity: 1, scale: 1 }}
                  className="text-xs text-orange-600 dark:text-orange-400"
                >
                  Done
                </motion.span>
              )}
            </motion.div>
          )
        })}
      </AnimatePresence>
    </div>
  )
}
