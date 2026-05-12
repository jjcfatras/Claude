// prettier.config.ts, .prettierrc.ts, prettier.config.mts, or .prettierrc.mts

import { type Config } from "prettier";

const config: Config = {
  trailingComma: "all",
  semi: true,
  printWidth: 80,
  plugins: ["prettier-plugin-sh"],
};

export default config;
