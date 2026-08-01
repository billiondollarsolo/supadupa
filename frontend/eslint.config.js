import js from "@eslint/js";
import jsxA11y from "eslint-plugin-jsx-a11y";
import react from "eslint-plugin-react";
import globals from "globals";
import tseslint from "typescript-eslint";

/** @type {import("eslint").Linter.Config[]} */
export default tseslint.config(
  {
    ignores: ["dist/**", "node_modules/**", "playwright-report/**", "test-results/**", "coverage/**"],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["src/**/*.{js,jsx,ts,tsx}"],
    plugins: {
      "jsx-a11y": jsxA11y,
      react,
    },
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: "module",
      globals: {
        ...globals.browser,
        ...globals.es2021,
      },
      parserOptions: {
        ecmaFeatures: { jsx: true },
      },
    },
    settings: {
      react: { version: "detect" },
    },
    rules: {
      // Plan G5: jsx-a11y recommended, tuned for Field/Input primitives and modal dialogs.
      ...jsxA11y.configs.recommended.rules,
      "jsx-a11y/label-has-associated-control": [
        "error",
        {
          // Nesting is the primary association pattern in this UI.
          assert: "either",
          depth: 5,
          labelComponents: ["Field", "label"],
          labelAttributes: ["label"],
          controlComponents: [
            "Input",
            "NativeSelect",
            "Textarea",
            "Select",
            "Switch",
            "Checkbox",
            "Button",
            "input",
            "select",
            "textarea",
            "button",
          ],
        },
      ],
      // Dialog/modal shells use keyboard handlers on role=dialog; role=presentation backdrop is intentional.
      "jsx-a11y/no-noninteractive-element-interactions": "off",
      "jsx-a11y/interactive-supports-focus": "off",
      // Confirm dialogs focus the confirmation field intentionally.
      "jsx-a11y/no-autofocus": "off",
      "@typescript-eslint/no-unused-vars": "off",
      "@typescript-eslint/no-explicit-any": "off",
      "@typescript-eslint/no-empty-object-type": "off",
      "no-unused-vars": "off",
      "no-undef": "off",
      "no-extra-boolean-cast": "off",
      "react/react-in-jsx-scope": "off",
      "react/jsx-uses-react": "off",
    },
  },
);
