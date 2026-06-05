import { type ReactNode, useEffect } from "react";
import { X } from "lucide-react";

export function Modal({
  children,
  description,
  footer,
  onClose,
  open,
  title,
}: {
  open: boolean;
  title: string;
  description?: string;
  children?: ReactNode;
  footer?: ReactNode;
  onClose: () => void;
}) {
  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [onClose, open]);

  if (!open) {
    return null;
  }

  return (
    <div className="modal-backdrop" onMouseDown={onClose} role="presentation">
      <section aria-modal="true" className="modal-dialog" onMouseDown={(event) => event.stopPropagation()} role="dialog">
        <div className="modal-head">
          <div className="min-w-0">
            <h2>{title}</h2>
            {description ? <p>{description}</p> : null}
          </div>
          <button className="icon-button h-8 min-h-8 min-w-8" onClick={onClose} title="Close" type="button">
            <X size={14} />
          </button>
        </div>
        {children ? <div className="modal-body">{children}</div> : null}
        {footer ? <div className="modal-footer">{footer}</div> : null}
      </section>
    </div>
  );
}
