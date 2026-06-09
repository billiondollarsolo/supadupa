export function focusableElements(root: HTMLElement | null) {
  if (!root) {
    return [];
  }
  return Array.from(
    root.querySelectorAll<HTMLElement>(
      'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ),
  ).filter((element) => !element.hasAttribute("disabled") && element.offsetParent !== null);
}

type InertElement = HTMLElement & { inert: boolean };

type InertSnapshot = {
  ariaHidden: string | null;
  element: InertElement;
  inert: boolean;
};

export function makeBackgroundInert(root: HTMLElement | null) {
  if (!root) {
    return () => undefined;
  }

  const snapshots = new Map<HTMLElement, InertSnapshot>();
  let current: HTMLElement | null = root;

  while (current && current !== document.body) {
    const parent: HTMLElement | null = current.parentElement;
    if (!parent) {
      break;
    }
    for (const child of Array.from(parent.children)) {
      if (child === current || !(child instanceof HTMLElement)) {
        continue;
      }
      const element = child as InertElement;
      if (!snapshots.has(element)) {
        snapshots.set(element, {
          ariaHidden: element.getAttribute("aria-hidden"),
          element,
          inert: element.inert,
        });
      }
      element.inert = true;
      element.setAttribute("aria-hidden", "true");
    }
    current = parent;
  }

  return () => {
    for (const { ariaHidden, element, inert } of Array.from(snapshots.values()).reverse()) {
      element.inert = inert;
      if (ariaHidden === null) {
        element.removeAttribute("aria-hidden");
      } else {
        element.setAttribute("aria-hidden", ariaHidden);
      }
    }
  };
}
