import { Component, type ErrorInfo, type ReactNode } from "react";
import { Button } from "./ui/button";

export type ErrorBoundaryProps = {
  children: ReactNode;
  /** Optional custom title for the fallback UI. */
  title?: string;
  /** Optional custom message for the fallback UI. */
  message?: string;
  /** When true (default), show a Reload page button. */
  showReload?: boolean;
  /** Optional callback when an error is caught (e.g. for logging). */
  onError?: (error: Error, info: ErrorInfo) => void;
};

type ErrorBoundaryState = {
  error: Error | null;
};

/**
 * Root-friendly React error boundary. Catches render errors in the subtree
 * and shows a calm fallback instead of a blank screen.
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    this.props.onError?.(error, info);
  }

  private handleReload = (): void => {
    window.location.reload();
  };

  private handleRetry = (): void => {
    this.setState({ error: null });
  };

  render(): ReactNode {
    const { error } = this.state;
    if (!error) {
      return this.props.children;
    }

    const title = this.props.title ?? "Something went wrong";
    const message =
      this.props.message ??
      "An unexpected error occurred while rendering this page. You can try again or reload.";
    const showReload = this.props.showReload !== false;

    return (
      <div
        className="grid min-h-[40vh] place-items-center px-4 py-12"
        data-testid="error-boundary-fallback"
        role="alert"
      >
        <div className="w-full max-w-md rounded-lg border border-border bg-surface p-6 shadow-sm">
          <h1 className="text-lg font-semibold text-text">{title}</h1>
          <p className="mt-2 text-sm text-muted">{message}</p>
          {import.meta.env.DEV && error.message ? (
            <pre className="mt-3 max-h-32 overflow-auto rounded-md border border-border bg-bg p-2 text-xs text-faint">
              {error.message}
            </pre>
          ) : null}
          <div className="mt-4 flex flex-wrap gap-2">
            <Button onClick={this.handleRetry} type="button" variant="secondary">
              Try again
            </Button>
            {showReload ? (
              <Button onClick={this.handleReload} type="button">
                Reload page
              </Button>
            ) : null}
          </div>
        </div>
      </div>
    );
  }
}
