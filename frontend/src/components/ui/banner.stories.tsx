import type { Meta, StoryObj } from "@storybook/react";

import { Banner } from "./banner";

const meta: Meta<typeof Banner> = {
  title: "Primitives/Banner",
  component: Banner,
  argTypes: {
    tone: {
      control: "select",
      options: ["info", "warning", "danger", "success"],
    },
    dismissible: { control: "boolean" },
  },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: { children: "Snapshot failed; retrying in 5s." },
};

export const Tones: Story = {
  render: () => (
    <div className="flex w-[480px] flex-col gap-3">
      <Banner tone="info">Informational message.</Banner>
      <Banner tone="warning">Warning — compaction backlog growing.</Banner>
      <Banner tone="danger">
        Error — snapshot endpoint returned 500.
      </Banner>
      <Banner tone="success">Compaction completed.</Banner>
    </div>
  ),
};

export const Dismissible: Story = {
  args: { dismissible: true, children: "Click X to dismiss." },
};
