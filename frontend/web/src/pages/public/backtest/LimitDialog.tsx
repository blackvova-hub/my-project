import { useEffect, useId, useRef } from "react";
import { Icon } from "./ui";

export function LimitDialog({ message, onClose }: { message: string; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const descriptionId = useId();
  useEffect(() => {
    const dialog = ref.current;
    if (!message || !dialog) return;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    dialog.showModal();
    return () => {
      dialog.close();
      document.body.style.overflow = previousOverflow;
    };
  }, [message]);

  return (
    <dialog
      ref={ref}
      className="bt-panel bt-limit-dialog"
      aria-labelledby={titleId}
      aria-describedby={descriptionId}
      onCancel={(event) => { event.preventDefault(); onClose(); }}
      onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}
    >
      <div className="bt-limit-content">
        <div className="bt-group-heading">
          <h2 id={titleId}>Возможности теста</h2>
          <button type="button" className="bt-icon-button" aria-label="Закрыть окно" onClick={onClose}>
            <Icon name="close" />
          </button>
        </div>
        <p id={descriptionId}>{message}</p>
        <button type="button" className="bt-button" onClick={onClose} autoFocus>Понятно</button>
      </div>
    </dialog>
  );
}
