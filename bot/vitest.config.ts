import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    globalSetup: "./src/vitest.global-setup.ts",
    include: ["src/**/*.test.ts"],
    passWithNoTests: true,
  },
});
