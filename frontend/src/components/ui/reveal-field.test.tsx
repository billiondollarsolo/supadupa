import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { RevealField } from "./reveal-field";

describe("RevealField", () => {
  it("reveals and copies the materialized value instead of the placeholder handle", async () => {
    const user = userEvent.setup();
    const originalClipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    const onReveal = vi.fn().mockResolvedValue("real-api-key");
    const onCopy = vi.fn();

    render(
      <RevealField
        label="anon"
        onCopy={onCopy}
        onReveal={onReveal}
        sensitive
        value="secret://projects/test-one/anon_key"
      />,
    );

    await user.click(screen.getByRole("button", { name: "Reveal value" }));
    expect(await screen.findByText("real-api-key")).toBeTruthy();
    expect(onReveal).toHaveBeenCalledTimes(1);

    await user.click(screen.getByRole("button", { name: "Copy value" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("real-api-key"));
    expect(writeText).not.toHaveBeenCalledWith("secret://projects/test-one/anon_key");
    expect(onReveal).toHaveBeenCalledTimes(1);
    expect(onCopy).toHaveBeenCalledTimes(1);

    if (originalClipboard) {
      Object.defineProperty(navigator, "clipboard", originalClipboard);
    } else {
      Reflect.deleteProperty(navigator, "clipboard");
    }
  });
});
