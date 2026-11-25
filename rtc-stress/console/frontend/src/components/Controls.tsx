import {
  cloneElement,
  isValidElement,
  useId,
  useState,
  type ChangeEvent,
  type InputHTMLAttributes,
  type ReactNode,
} from "react";
import { Form, InputNumber, Tag } from "antd";
import { states } from "../types";
export { Icon } from "./Icons";
export function Field({
  label,
  hint,
  error,
  required,
  children,
}: {
  label: string;
  hint?: string;
  error?: string;
  required?: boolean;
  children: ReactNode;
}) {
  const generatedId = useId();
  const child = isValidElement<{ id?: string; "aria-describedby"?: string }>(
    children,
  )
    ? children
    : null;
  const id = child?.props.id || generatedId;
  const helpId = generatedId + "-help";
  return (
    <Form.Item
      className="field"
      label={label}
      htmlFor={id}
      required={required}
      validateStatus={error ? "error" : undefined}
      help={error ? <span id={helpId}>{error}</span> : undefined}
      extra={hint && !error ? <span id={helpId}>{hint}</span> : undefined}
    >
      {child
        ? cloneElement(child, {
            id,
            "aria-describedby":
              [child.props["aria-describedby"], error || hint ? helpId : ""]
                .filter(Boolean)
                .join(" ") || undefined,
          })
        : children}
    </Form.Item>
  );
}
export function NumericInput({
  value,
  onChange,
  onFocus,
  onBlur,
  min,
  max,
  disabled,
  id,
  className,
  style,
  readOnly,
  ...props
}: Omit<InputHTMLAttributes<HTMLInputElement>, "type" | "inputMode" | "size">) {
  const [draft, setDraft] = useState<number | null>(null),
    [focused, setFocused] = useState(false);
  const numeric = value === "" || value == null ? null : Number(value);
  return (
    <InputNumber<number>
      id={id}
      className={className}
      style={{ width: "100%", ...style }}
      min={min === undefined ? undefined : Number(min)}
      max={max === undefined ? undefined : Number(max)}
      disabled={disabled}
      readOnly={readOnly}
      controls={false}
      keyboard={false}
      changeOnBlur={false}
      changeOnWheel={false}
      precision={0}
      inputMode="numeric"
      aria-label={props["aria-label"]}
      aria-describedby={props["aria-describedby"]}
      aria-invalid={props["aria-invalid"]}
      value={focused ? draft : numeric}
      onFocus={(event) => {
        setDraft(numeric);
        setFocused(true);
        onFocus?.(event);
      }}
      onChange={(next) => {
        setDraft(next);
        onChange?.({
          target: { value: next == null ? "" : String(next) },
        } as ChangeEvent<HTMLInputElement>);
      }}
      onBlur={(event) => {
        setFocused(false);
        onBlur?.(event);
      }}
    />
  );
}
export function StateTag({ state }: { state?: string }) {
  const value = state || "pending";
  const color =
    value === "running" || value === "completed"
      ? "success"
      : value === "failed" || value === "interrupted"
        ? "error"
        : value === "stopped" || value === "stopping"
          ? "warning"
          : value === "pending" || value === "exited"
            ? "default"
            : "processing";
  return (
    <Tag color={color} variant="filled">
      {states[value] || value}
    </Tag>
  );
}
