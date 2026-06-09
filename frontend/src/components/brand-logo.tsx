type BrandLogoProps = {
  className?: string;
};

// The brand mark is a self-contained purple tile with a white glyph (public/logo.svg),
// so it renders as an image rather than inheriting text color.
export function BrandLogo({ className = "" }: BrandLogoProps) {
  return <img alt="Supadupa" className={className} src="/logo.svg" />;
}
