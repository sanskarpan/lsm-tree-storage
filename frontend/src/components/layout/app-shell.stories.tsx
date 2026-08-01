import type { Meta, StoryObj } from "@storybook/react";

import { AppShell } from "../layout";

const meta: Meta<typeof AppShell> = {
  title: "Layout/AppShell",
  component: AppShell,
  parameters: {
    docs: {
      description: {
        component: "The top-level layout shell. Uses the Zustand store for all data — run with a live backend to see real content.",
      },
    },
  },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  render: () => <AppShell />,
};
