import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
  ]),
  {
    // This app's pages synchronize with the API server by polling inside
    // effects (fetch helper -> setState in async callbacks, plus
    // setInterval). That is exactly what effects are for, but
    // react-hooks/set-state-in-effect cannot see through the async fetch
    // helpers and flags the idiomatic polling pattern, so it is disabled.
    // The two genuinely synchronous resets (clearing a detail panel when
    // the selection is cleared) are harmless single assignments.
    rules: {
      "react-hooks/set-state-in-effect": "off",
    },
  },
]);

export default eslintConfig;
