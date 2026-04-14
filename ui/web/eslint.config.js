import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist", "node_modules"] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: 2024,
      globals: globals.browser,
    },
    plugins: {
      "react-hooks": reactHooks,
      "react-refresh": reactRefresh,
    },
    rules: {
      // React hooks - keep critical rules only
      "react-hooks/rules-of-hooks": "error",
      // Disabled: many intentional patterns (stable refs, translation fn)
      "react-hooks/exhaustive-deps": "off",
      // Disabled: legitimate pattern to co-locate hooks with components
      "react-refresh/only-export-components": "off",

      // TypeScript
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_" }],
      // Disabled: pragmatic casts for external libs and dynamic data
      "@typescript-eslint/no-explicit-any": "off",

      // General - disabled for dev convenience
      "no-console": "off",
    },
  }
);
