type BrandWordmarkProps = {
  className?: string;
};

export function BrandWordmark({ className = "" }: BrandWordmarkProps) {
  return <span className={`wordmark ${className}`.trim()}>SUPADUPA</span>;
}
