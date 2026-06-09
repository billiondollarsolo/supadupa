import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { Modal } from "./modal";

describe("Modal keyboard behavior", () => {
  let offsetParentDescriptor: PropertyDescriptor | undefined;

  beforeAll(() => {
    offsetParentDescriptor = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetParent");
    Object.defineProperty(HTMLElement.prototype, "offsetParent", {
      configurable: true,
      get() {
        return document.body;
      },
    });
  });

  afterAll(() => {
    if (offsetParentDescriptor) {
      Object.defineProperty(HTMLElement.prototype, "offsetParent", offsetParentDescriptor);
    }
  });

  it("traps focus, inerts background content, closes on Escape, and restores focus", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    const { container } = render(<ModalHarness onClose={onClose} />);

    const trigger = screen.getByRole("button", { name: "Open danger modal" });
    const outside = container.querySelector<HTMLButtonElement>('[data-testid="background-action"]');
    expect(outside).not.toBeNull();
    await user.click(trigger);

    expect(outside?.getAttribute("aria-hidden")).toBe("true");
    expect((outside as HTMLElement & { inert?: boolean }).inert).toBe(true);

    const close = screen.getByRole("button", { name: "Close" });
    const destructive = screen.getByRole("button", { name: "Delete project" });
    await waitFor(() => expect(document.activeElement).toBe(close));

    await user.tab({ shift: true });
    expect(document.activeElement).toBe(destructive);

    await user.tab();
    expect(document.activeElement).toBe(close);

    await user.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(1);

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(outside?.getAttribute("aria-hidden")).toBeNull();
    expect((outside as HTMLElement & { inert?: boolean }).inert).not.toBe(true);
    expect(document.activeElement).toBe(trigger);
  });
});

function ModalHarness({ onClose }: { onClose: () => void }) {
  const [open, setOpen] = useState(false);
  const close = () => {
    onClose();
    setOpen(false);
  };
  return (
    <div>
      <button data-testid="background-action" type="button">
        Background action
      </button>
      <button onClick={() => setOpen(true)} type="button">
        Open danger modal
      </button>
      <Modal
        description="This action cannot be undone."
        footer={
          <>
            <button onClick={close} type="button">
              Cancel
            </button>
            <button onClick={close} type="button">
              Delete project
            </button>
          </>
        }
        onClose={close}
        open={open}
        title="Delete project"
      >
        <p>Deleting the project removes runtime state.</p>
      </Modal>
    </div>
  );
}
