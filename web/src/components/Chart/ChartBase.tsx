import { forwardRef } from "react";
import type { CSSProperties, KeyboardEventHandler, ReactNode } from "react";

export interface ChartBaseProps {
  children?: ReactNode;
  className?: string;
  height?: number;
  ariaLabel: string;
  tabIndex?: number;
  onKeyDown?: KeyboardEventHandler<HTMLDivElement>;
}

export const ChartBase = forwardRef<HTMLDivElement, ChartBaseProps>(function ChartBase({ children, className, height = 280, ariaLabel, tabIndex, onKeyDown }, ref) {
  const style: CSSProperties = { height, minHeight: height };
  return <div aria-label={ariaLabel} className={className} onKeyDown={onKeyDown} ref={ref} role="img" style={style} tabIndex={tabIndex}>{children}</div>;
});
