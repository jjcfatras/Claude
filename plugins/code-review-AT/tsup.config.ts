import { defineConfig } from "tsup";

export default defineConfig({
  entry: ["src/cli.ts"],
  format: ["esm"],
  target: "node22",
  outDir: "dist",
  bundle: true,
  minify: true,
  splitting: false,
  sourcemap: false,
  clean: true,
  noExternal: ["@anthropic-ai/claude-agent-sdk", "zod", "zod-to-json-schema"],
  banner: { js: "#!/usr/bin/env node" },
});
