import type { Meta, StoryObj } from "@storybook/react";

import { AmplificationDeck } from "./AmplificationDeck";
import type { AmpPoint } from "../../types";

const meta: Meta<typeof AmplificationDeck> = {
  title: "Panels/AmplificationDeck",
  component: AmplificationDeck,
  decorators: [
    (Story) => (
      <div className="max-w-2xl">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof meta>;

const now = Date.now();
const history: AmpPoint[] = Array.from({ length: 12 }, (_, i) => ({
  wa: 1 + (i * 0.4) % 6,
  ra: 0.5 + (i * 0.3) % 3,
  sa: 1.2 + (i * 0.2) % 2,
  timestamp: now - (12 - i) * 60_000,
}));

export const Stable: Story = {
  args: { wa: 2.1, ra: 0.8, sa: 1.4, history: [] },
};

export const Elevated: Story = {
  args: { wa: 9.5, ra: 4.2, sa: 2.6, history: [] },
};

export const WithHistory: Story = {
  args: { wa: 3.4, ra: 1.6, sa: 1.9, history },
};
