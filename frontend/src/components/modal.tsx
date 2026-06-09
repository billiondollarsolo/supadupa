import { type KeyboardEvent, type ReactNode, useEffect, useId, useRef } from "react";
import { X } from "lucide-react";
import { focusableElements, makeBackgroundInert } from "../lib/focus";

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
  const titleID = useId();
  const descriptionID = useId();
  const backdropRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!open) return;
    previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const restoreBackground = makeBackgroundInert(backdropRef.current);
    window.setTimeout(() => {
      const first = focusableElements(dialogRef.current)[0] ?? dialogRef.current;
      first?.focus();
    }, 0);
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      restoreBackground();
      if (previousFocusRef.current?.isConnected) {
        previousFocusRef.current.focus();
      }
      previousFocusRef.current = null;
    };
  }, [onClose, open]);

  if (!open) {
    return null;
  }

  function onDialogKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (event.key !== "Tab") {
      return;
    }
    const elements = focusableElements(dialogRef.current);
    if (elements.length === 0) {
      event.preventDefault();
      return;
    }
    const first = elements[0];
    const last = elements[elements.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
      return;
    }
    if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  return (
    <div className="modal-backdrop" onMouseDown={onClose} ref={backdropRef} role="presentation">
      <section aria-describedby={description ? descriptionID : undefined} aria-labelledby={titleID} aria-modal="true" className="modal-dialog" onKeyDown={onDialogKeyDown} onMouseDown={(event) => event.stopPropagation()} ref={dialogRef} role="dialog" tabIndex={-1}>
        <div className="modal-head">
          <div className="min-w-0">
            <h2 id={titleID}>{title}</h2>
            {description ? <p id={descriptionID}>{description}</p> : null}
          </div>
          <button aria-label="Close" className="icon-button h-8 min-h-8 min-w-8" onClick={onClose} title="Close" type="button">
            <X size={14} />
          </button>
        </div>
        {children ? <div className="modal-body">{children}</div> : null}
        {footer ? <div className="modal-footer">{footer}</div> : null}
      </section>
    </div>
  );
}
