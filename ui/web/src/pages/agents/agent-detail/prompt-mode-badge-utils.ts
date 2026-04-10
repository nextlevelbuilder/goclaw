/** Returns badge CSS classes based on prompt mode value. */
export function promptModeBadgeClass(mode: string): string {
  switch (mode) {
    case "task":
      return "border-purple-300 text-purple-700 dark:border-purple-700 dark:text-purple-300";
    case "minimal":
      return "border-amber-300 text-amber-700 dark:border-amber-700 dark:text-amber-300";
    case "none":
      return "border-gray-300 text-gray-500 dark:border-gray-600 dark:text-gray-400";
    default:
      return "";
  }
}
