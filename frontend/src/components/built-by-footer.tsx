// Shared attribution used on the login screen and the logged-in sidebar.
// Kept in one place so the handles/links stay in sync with the About page.
export const BUILDERS = [
  { handle: "mjtechguy", url: "https://x.com/mjtechguy" },
  { handle: "blndollarsolo", url: "https://x.com/blndollarsolo" },
] as const;

export const REPO_URL = "https://github.com/billiondollarsolo/supadupa";

type BuiltByFooterProps = {
  className?: string;
};

export function BuiltByFooter({ className = "" }: BuiltByFooterProps) {
  return (
    <p className={`text-xs text-faint ${className}`.trim()}>
      Built by{" "}
      {BUILDERS.map((builder, index) => (
        <span key={builder.handle}>
          {index > 0 ? " and " : null}
          <a
            className="text-muted underline-offset-2 hover:text-text hover:underline"
            href={builder.url}
            rel="noreferrer noopener"
            target="_blank"
          >
            @{builder.handle}
          </a>
        </span>
      ))}
    </p>
  );
}
