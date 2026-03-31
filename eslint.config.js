const tsParser = require("@typescript-eslint/parser");

module.exports = [
  {
    ignores: [
      "**/dist/**",
      "**/node_modules/**",
      "wrapper/target/**",
    ],
  },
  {
    files: ["**/*.ts"],
    languageOptions: {
      parser: tsParser,
      ecmaVersion: "latest",
      sourceType: "module",
    },
    rules: {},
  },
];
