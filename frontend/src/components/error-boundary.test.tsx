import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ErrorBoundary } from "./error-boundary";

function ThrowingChild({ shouldThrow }: { shouldThrow: boolean }) {
  if (shouldThrow) {
    throw new Error("forced render failure");
  }
  return <p data-testid="healthy-child">child ok</p>;
}

describe("ErrorBoundary", () => {
  let consoleErrorSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    // React logs expected boundary errors to console.error; silence noise in test output.
    consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
  });

  afterEach(() => {
    cleanup();
    consoleErrorSpy.mockRestore();
  });

  it("renders children when no error is thrown", () => {
    render(
      <ErrorBoundary>
        <ThrowingChild shouldThrow={false} />
      </ErrorBoundary>,
    );

    expect(screen.getByTestId("healthy-child")).toBeTruthy();
    expect(screen.queryByTestId("error-boundary-fallback")).toBeNull();
  });

  it("catches a child throw and renders the friendly fallback", () => {
    render(
      <ErrorBoundary showReload={false}>
        <ThrowingChild shouldThrow />
      </ErrorBoundary>,
    );

    expect(screen.getByTestId("error-boundary-fallback")).toBeTruthy();
    expect(screen.getByRole("alert")).toBeTruthy();
    expect(screen.getByText("Something went wrong")).toBeTruthy();
    expect(
      screen.getByText(
        "An unexpected error occurred while rendering this page. You can try again or reload.",
      ),
    ).toBeTruthy();
    expect(screen.queryByTestId("healthy-child")).toBeNull();
  });

  it("supports custom title and message", () => {
    render(
      <ErrorBoundary message="Custom detail message." showReload={false} title="Custom title">
        <ThrowingChild shouldThrow />
      </ErrorBoundary>,
    );

    expect(screen.getByText("Custom title")).toBeTruthy();
    expect(screen.getByText("Custom detail message.")).toBeTruthy();
  });
});