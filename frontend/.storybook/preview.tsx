import type { Preview } from "@storybook/react";
import { withThemeByClassName } from "@storybook/addon-themes";

import "../src/styles/base.css";

const preview: Preview = {
  parameters: {
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
    layout: "padded",
  },
  decorators: [
    withThemeByClassName({
      themes: {
        light: "",
        dark: "dark",
        compact: "compact",
      },
      defaultTheme: "light",
    }),
    (Story) => (
      <div className="bg-[var(--bg)] text-[var(--fg)] min-h-screen p-4">
        <Story />
      </div>
    ),
  ],
};

export default preview;
