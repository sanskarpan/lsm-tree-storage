import type { Meta, StoryObj } from "@storybook/react";

import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "./card";

const meta: Meta<typeof Card> = {
  title: "Primitives/Card",
  component: Card,
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  render: () => (
    <Card className="max-w-md">
      <CardHeader>
        <CardTitle>Notifications</CardTitle>
        <CardDescription>You have 3 unread messages.</CardDescription>
      </CardHeader>
      <CardContent>
        <p>Card body content goes here.</p>
      </CardContent>
      <CardFooter className="flex justify-end gap-2">
        <button className="text-sm text-[var(--fg-muted)]">Dismiss</button>
        <button className="text-sm text-[var(--accent)]">View all</button>
      </CardFooter>
    </Card>
  ),
};
